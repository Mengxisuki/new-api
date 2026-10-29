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

import "github.com/gin-gonic/gin"

// 采购图片独立于徽标及微信登录配置，展示键继续沿用现有公开状态字段。
// 数据键保持原值，已上传的图片无需迁移或重新上传。
const (
	procurementQRCodeDataOption = "ProcurementWeChatQRCodeImageData"
	procurementQRCodePath       = "/api/status/procurement-wechat-qr"
)

// 仅在服务端选择固定配置；上传参数不能决定写入其他图片或业务字段。
var procurementQRCodeConfig = optionImageConfig{
	dataKey: procurementQRCodeDataOption,  // 编码不进入通用设置响应。
	urlKey:  "ProcurementWeChatQRCodeURL", // 保持原首页和表单读取契约。
	path:    procurementQRCodePath,        // 老图片公开地址不变。
}

// UploadProcurementQRCode 复用设置图片校验和原子持久化。
// 专用入口仍由路由层要求超级管理员身份。
func UploadProcurementQRCode(c *gin.Context) {
	uploadOptionImage(c, procurementQRCodeConfig)
}

// DeleteProcurementQRCode 只清空采购图片，不清空姓名及微信号。
// 共用删除路径同时处理编码与公开地址，失败保留原配置。
func DeleteProcurementQRCode(c *gin.Context) {
	deleteOptionImage(c, procurementQRCodeConfig)
}

// GetProcurementQRCode 公开读取图片，保持原缓存及内容响应约束。
// 返回原始图片字节，不向访问者公开内部存储编码。
func GetProcurementQRCode(c *gin.Context) {
	getOptionImage(c, procurementQRCodeConfig)
}
