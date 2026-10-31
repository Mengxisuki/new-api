package controller

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// 附件只有受保护下载入口，不使用公开upload目录或管理员提供的任意URL。
// 容量同时限制请求体、上传头与实际读取，避免大文件占满内存或磁盘。
const invoiceMaxFileBytes = 5 << 20

// UploadInvoiceFile接收已付款申请的PDF/图片，并在持久化成功后标记已开具。
// 文件先写入私有临时文件；数据库失败时删除，避免留下孤立发票。
// 仅能开具一次，不提供覆盖已开具附件的隐式重开路径。
func UploadInvoiceFile(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id")) // 数字申请ID不参与文件路径拼接。
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	invoice, err := model.InvoiceByID(id, 0) // 财务查询已由路由细粒度权限保护。
	if err != nil {
		common.ApiErrorT(c, "Invoice request not found")
		return
	}
	if invoice.Status != "pending" && invoice.Status != "processing" {
		common.ApiErrorT(c, "Only paid pending invoice requests can receive a file")
		return
	}
	data, mimeType, err := readInvoiceUpload(c) // 先验证内容再写文件，扩展名不能伪造类型。
	if err != nil {
		common.ApiError(c, err)
		return
	}
	path, err := savePrivateInvoiceFile(id, data) // 私有文件不会得到可公开访问的URL。
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.UpdateInvoiceRequest(id, model.InvoiceProcessing{Status: "issued", InvoiceNumber: c.PostForm("invoice_number"), Remark: c.PostForm("remark"), FilePath: path, FileMime: mimeType})
	if err != nil {
		discardInvoiceFile(path) // 数据库拒绝或并发终态冲突时回收此次临时文件。
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// readInvoiceUpload是附件进入系统的唯一解析边界，扩展名不作为可信类型。
// 限制整个multipart与实际文件内容，拒绝空文件、伪装类型和超大图片。
// 图片只解码尺寸头，避免为了验证而分配完整超大像素缓冲。
func readInvoiceUpload(c *gin.Context) ([]byte, string, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, invoiceMaxFileBytes+(64<<10)) // 包含multipart头部也有上限。
	header, err := c.FormFile("file")                                                            // 接口固定字段名，不能选择任意服务端文件。
	if err != nil || header.Size > invoiceMaxFileBytes {
		return nil, "", common.NewMessage("Invoice file must be a PDF, PNG or JPEG up to 5 MiB")
	}
	source, err := header.Open() // 文件可能位于内存或框架临时目录，读取方式相同。
	if err != nil {
		return nil, "", common.NewMessage("Failed to read invoice file")
	}
	defer source.Close()                                                   // 请求结束释放上传句柄，避免累计耗尽文件描述符。
	data, err := io.ReadAll(io.LimitReader(source, invoiceMaxFileBytes+1)) // 多读一个字节用于发现虚报大小。
	if err != nil || len(data) == 0 || len(data) > invoiceMaxFileBytes {
		return nil, "", common.NewMessage("Invoice file must be a PDF, PNG or JPEG up to 5 MiB")
	}
	mimeType := http.DetectContentType(data) // 不信任浏览器Content-Type及上传文件扩展名。
	if mimeType != "application/pdf" && mimeType != "image/png" && mimeType != "image/jpeg" {
		return nil, "", common.NewMessage("Invoice file must be a PDF, PNG or JPEG up to 5 MiB")
	}
	if strings.HasPrefix(mimeType, "image/") {
		config, _, err := image.DecodeConfig(bytes.NewReader(data)) // 只解码头部即可拒绝像素炸弹。
		if err != nil || config.Width > 10000 || config.Height > 10000 {
			return nil, "", common.NewMessage("Invalid invoice image dimensions")
		}
	}
	return data, mimeType, nil // 后续层遵从此可信内容契约，不再重复猜测文件类型。
}

// savePrivateInvoiceFile使用随机文件名与0600权限，不信任上传文件名或路径。
// 工作目录是持久数据目录，私有子目录不由静态路由暴露。
// 所有写入与关闭错误都使本次上传失败，不能伪造已保存附件。
func savePrivateInvoiceFile(id int, data []byte) (string, error) {
	directory := filepath.Join("data", "invoices")       // 位于部署工作数据目录，不放入web静态资源。
	if err := os.MkdirAll(directory, 0700); err != nil { // 只有应用运行账户能遍历这个目录。
		return "", common.NewMessage("Failed to save invoice file")
	}
	file, err := os.CreateTemp(directory, fmt.Sprintf("invoice-%d-", id)) // 随机名及0600权限由标准库生成。
	if err != nil {
		return "", common.NewMessage("Failed to save invoice file")
	}
	path, err := filepath.Abs(file.Name()) // 持久化绝对路径，不受随后请求的相对路径影响。
	if err != nil {
		closeErr := file.Close() // 延迟的文件系统错误也不能被当成成功。
		if closeErr != nil {
			common.SysError(common.LogText("failed to close invoice temporary file: %v", closeErr))
		}
		discardInvoiceFile(file.Name())
		return "", common.NewMessage("Failed to save invoice file")
	}
	_, writeErr := file.Write(data) // 必须同时检查写入和关闭结果才认为保存完成。
	closeErr := file.Close()        // 延迟的文件系统错误也不能被当成成功。
	if writeErr != nil || closeErr != nil {
		discardInvoiceFile(path) // 数据库拒绝或并发终态冲突时回收此次临时文件。
		return "", common.NewMessage("Failed to save invoice file")
	}
	return path, nil // 只有真实保存成功的附件才进入数据库状态更新。
}

// 数据库未接受附件时回收临时文件；删除失败留可定位日志，不吞错。
func discardInvoiceFile(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		common.SysError(common.LogText("failed to discard invoice temporary file: %v", err))
	}
}

// 下载始终按数据库文件引用读取，不接受客户端提供文件名或本地路径。
// Content-Disposition附件模式和nosniff阻止浏览器以内联HTML执行上传内容。
// 本人接口会把用户ID加到查询，财务接口的全量读取由路由权限保护。
// 未开具、缺文件和无权限记录统一为404，不允许借下载入口枚举申请。
// 原始上传文件名不返回，文件名由申请ID和可信MIME生成。
// 下载不会改变开票状态，也不赋予用户读取其他申请的能力。
func downloadInvoiceFile(c *gin.Context, userID int) {
	c.Header("Cache-Control", "private, no-store") // 错误与成功响应都不缓存，避免切换账户复用下载结果。
	id, err := strconv.Atoi(c.Param("id"))         // 数字申请ID不参与文件路径拼接。
	if err != nil || id <= 0 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	invoice, err := model.InvoiceByID(id, userID) // 本人查询同时匹配所有权，不泄露他人记录存在性。
	if err != nil || invoice.Status != "issued" || invoice.FilePath == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	file, err := os.Open(invoice.FilePath) // 路径只来自应用写入过的数据库记录。
	if err != nil {
		common.ApiErrorT(c, "Invoice file is unavailable")
		return
	}
	defer file.Close()       // 下载结束释放私有文件，客户端拿不到磁盘位置。
	stat, err := file.Stat() // 不允许把目录或特殊设备作为发票内容返回。
	if err != nil || !stat.Mode().IsRegular() {
		common.ApiErrorT(c, "Invoice file is unavailable")
		return
	}
	ext := map[string]string{"application/pdf": ".pdf", "image/png": ".png", "image/jpeg": ".jpg"}[invoice.FileMime]
	c.Header("Content-Type", invoice.FileMime)     // 使用上传内容验证后的类型，而非请求参数。
	c.Header("X-Content-Type-Options", "nosniff")  // 禁止浏览器猜成可执行HTML。
	c.Header("Cache-Control", "private, no-store") // 私有税务资料不能进入公共缓存。
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fmt.Sprintf("invoice-%d%s", invoice.Id, ext)}))
	http.ServeContent(c.Writer, c.Request, "", stat.ModTime(), file) // 原生流式传输，不把附件再次完整复制进内存。
}

// 两个入口保持相同下载实现，只有已授权查询范围不同。
func DownloadUserInvoiceFile(c *gin.Context)    { downloadInvoiceFile(c, c.GetInt("id")) }
func DownloadFinanceInvoiceFile(c *gin.Context) { downloadInvoiceFile(c, 0) }
