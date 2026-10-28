/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"golang.org/x/image/webp"
)

// 二维码独立于微信登录配置，替换采购图片不会改变认证流程。
// 内部编码键仅供图片处理器读取，不出现在通用设置列表中。
// 固定公开路径使前端无需了解图片在配置中的存储形式。
// 文件大小限制约束上传和数据库存储成本。
// 尺寸上限通过图片头读取，避免为检查尺寸完整解码大图。
const (
	procurementQRCodeDataOption = "ProcurementWeChatQRCodeImageData"
	procurementQRCodePath       = "/api/status/procurement-wechat-qr"
	procurementQRCodeMaxBytes   = 1 << 20
	procurementQRCodeMaxSide    = 4096
)

// UploadProcurementQRCode 接收超级管理员上传的采购联系二维码。
// 路由层复用现有 RootAuth，本函数只处理已授权请求的图片输入。
// 图片和公开地址沿用通用配置存储，不增加独立表或迁移。
// 二者在同一个事务中提交，避免地址先更新而图片尚未持久化。
// 上传失败保持原配置，调用方从 success 和错误消息识别结果。
// 返回值是图片公开地址，不包含存储编码或其他系统配置。
func UploadProcurementQRCode(c *gin.Context) {
	// 限制整个 multipart 请求，避免伪造文件长度绕过文件大小检查。
	// 额外预留 64 KiB 容纳 multipart 字段头及边界。
	// 请求读取失败会沿既有参数错误协议显式返回。
	c.Request.Body = http.MaxBytesReader(
		c.Writer,
		c.Request.Body,
		procurementQRCodeMaxBytes+(64<<10),
	)
	// 上传约定只有名为 file 的文件字段。
	// 缺少文件、错误 multipart 或超过请求上限均不继续写入。
	fileHeader, err := c.FormFile("file")
	if err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	// 上传头提供的大小用于尽早拒绝超限请求。
	// 后续仍会独立检查实际读取长度，两者不能互相替代。
	if fileHeader.Size > procurementQRCodeMaxBytes {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}

	// 文件来源可能是内存，也可能是 multipart 生成的临时文件。
	// 完成读取后及时关闭句柄，避免请求累计占用文件描述符。
	file, err := fileHeader.Open()
	if err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	defer file.Close()

	// 限制实际读取字节，而不只信任上传头声明的文件长度。
	// 多读一个字节用于区分恰好到达上限和超限文件。
	// 空文件与读取错误均不能覆盖现有二维码。
	data, err := io.ReadAll(io.LimitReader(file, procurementQRCodeMaxBytes+1))
	if err != nil || len(data) == 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if len(data) > procurementQRCodeMaxBytes {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}

	// 按内容识别格式，不信任文件扩展名或客户端 Content-Type。
	// SVG 等可执行内容不会通过白名单。
	// 先验证图片头，再存储原始字节，不重新编码图片。
	// 只读取头部可保留二维码原始清晰度并限制解码开销。
	mimeType := http.DetectContentType(data)
	var config image.Config
	switch mimeType {
	case "image/png", "image/jpeg":
		config, _, err = image.DecodeConfig(bytes.NewReader(data))
	case "image/webp":
		config, err = webp.DecodeConfig(bytes.NewReader(data))
	default:
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	// 两个维度分别限制，极窄但极高的图片同样拒绝。
	// 尺寸检查发生在任何配置写入之前。
	if config.Width > procurementQRCodeMaxSide || config.Height > procurementQRCodeMaxSide {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}

	// 内容摘要用于公开 URL 的缓存版本号。
	// 更换图片会产生新地址，浏览器无需手动刷新图片缓存。
	// MIME 与 Base64 以单个换行分隔，与读取处理器约定一致。
	// 原始图片编码不会混入其他配置或认证二维码字段。
	digest := sha256.Sum256(data)
	imageURL := fmt.Sprintf("%s?v=%x", procurementQRCodePath, digest[:8])
	encoded := mimeType + "\n" + base64.StdEncoding.EncodeToString(data)
	// 复用现有原子批量写入，同时更新持久化与内存配置。
	// 任意写入失败向调用方返回真实错误，不伪造上传成功。
	if err = model.UpdateOptionsBulk(map[string]string{
		procurementQRCodeDataOption:  encoded,
		"ProcurementWeChatQRCodeURL": imageURL,
	}); err != nil {
		common.ApiError(c, err)
		return
	}

	// 审计仅记录被修改的配置键，不记录二维码图片内容。
	recordManageAudit(c, "option.update", map[string]any{
		"key": "ProcurementWeChatQRCodeURL",
	})
	common.ApiSuccess(c, gin.H{"url": imageURL})
}

// DeleteProcurementQRCode 清空采购二维码及其公开地址。
// 联系人姓名与微信号保留，删除图片不会隐式清空其他联系信息。
// 图片和 URL 同步清空，避免公开页保留指向已删除图片的地址。
// 持久化失败必须报告；前端只有成功后才移除预览。
func DeleteProcurementQRCode(c *gin.Context) {
	// 批量更新保证清空图片与清空 URL 不会只成功一半。
	// 不删除配置记录，以沿用通用设置的空值语义。
	if err := model.UpdateOptionsBulk(map[string]string{
		procurementQRCodeDataOption:  "",
		"ProcurementWeChatQRCodeURL": "",
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "option.update", map[string]any{
		"key": "ProcurementWeChatQRCodeURL",
	})
	common.ApiSuccess(c, nil)
}

// GetProcurementQRCode 为匿名首页返回已发布的采购联系图片。
// 公开读请求只访问采购图片专用配置，不读取认证二维码。
// 响应直接发送图片字节，避免把内部 Base64 编码暴露给前端。
// 未配置、未知格式或损坏编码均返回 404，不能伪装成有效图片。
// 图片内容版本由上传返回地址的查询参数区分。
func GetProcurementQRCode(c *gin.Context) {
	// 读取时只在复制配置字符串期间持读锁。
	// 解码和响应写入在锁外进行，避免拖慢管理员更新配置。
	common.OptionMapRWMutex.RLock()
	encoded := common.OptionMap[procurementQRCodeDataOption]
	common.OptionMapRWMutex.RUnlock()

	// 即使数据来自配置存储，仍需按存储边界校验 MIME 和编码。
	// 仅接受上传处理器支持的三类图片。
	// 旧配置可能为空或损坏，不把这类内容按任意 MIME 发送。
	mimeType, base64Data, ok := strings.Cut(encoded, "\n")
	if !ok || (mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/webp") {
		c.Status(http.StatusNotFound)
		return
	}
	// 解码错误意味着配置不是可发送的图片，不返回半截数据。
	// 空编码同样视作未配置，避免生成成功但无法显示的图片响应。
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil || len(data) == 0 {
		c.Status(http.StatusNotFound)
		return
	}

	// 公开图片可被缓存，ETag 允许浏览器在重验证时省去响应体。
	// 缓存键使用完整存储编码，格式变化也会更新校验值。
	// nosniff 禁止浏览器根据图片字节猜测为其他可执行内容。
	// 缓存策略与旧分支保持一致，图片更新后使用新的内容版本 URL。
	etag := common.ETagFor("procurement-wechat-qr:v1", encoded)
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", etag)
	c.Header("X-Content-Type-Options", "nosniff")
	// 条件请求命中时只返回 304，不重复发送图片响应体。
	// 缓存头在判断之前设置，重验证结果也携带一致的缓存信息。
	if common.ETagMatches(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, mimeType, data)
}
