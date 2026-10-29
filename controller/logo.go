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

// 徽标以专用图片键保存原始内容，Logo 继续作为现有界面的公开地址。
// 不更改页头、页脚及 favicon 消费契约，也不涉及登录二维码。
const logoImageDataOption = "LogoImageData"

// 资源定义由服务端固定，不能由请求指定配置键或公开路径。
var logoImageConfig = optionImageConfig{
	dataKey: logoImageDataOption, // 内部编码禁止进入通用设置列表。
	urlKey:  "Logo",              // 现有 model 写入同时更新 common.Logo。
	path:    "/api/status/logo",  // 站内公开图片，内容摘要区分替换版本。
}

// UploadLogo 按采购二维码相同规则校验并立即保存徽标。
// 权限继承系统设置的 RootAuth，不开放匿名上传。
func UploadLogo(c *gin.Context) {
	uploadOptionImage(c, logoImageConfig)
}

// DeleteLogo 清空上传内容与展示地址，现有界面按原约定展示默认徽标。
// 删除成功前不清空内存配置，数据库错误正常返回。
func DeleteLogo(c *gin.Context) {
	deleteOptionImage(c, logoImageConfig)
}

// GetLogo 将已发布徽标作为静态图片公开读取。
// MIME 白名单、缓存和未配置响应复用同一实现。
func GetLogo(c *gin.Context) {
	getOptionImage(c, logoImageConfig)
}
