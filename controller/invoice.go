package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetInvoiceProfile只返回认证用户自己的当前抬头，未配置明确返回null。
// 税号和银行信息不并入公共用户接口，也不会被缓存到站点状态。
func GetInvoiceProfile(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")          // 税号和银行资料不允许浏览器或代理复用缓存。
	profile, err := model.GetInvoiceProfile(c.GetInt("id")) // 用户范围由认证中间件决定。
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, profile) // 未配置时保留null，不伪造空白抬头。
}

// PutInvoiceProfile限制JSON体大小后校验抬头，不接受请求中的所有者ID。
// 保存立即返回归一化结果，快捷弹窗和个人资料复用同一接口。
func PutInvoiceProfile(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10) // 限制个人税务表单的入站体积。
	var profile model.InvoiceProfile                                       // 不把抬头资料绑定进登录用户结构。
	if err := c.ShouldBindJSON(&profile); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	result, err := model.SaveInvoiceProfile(c.GetInt("id"), profile) // 模型会覆盖客户端伪造的user_id。
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result) // 只在数据库确认保存后返回更新资料。
}

// DeleteInvoiceProfile只删除当前配置，已存在订单保留独立的抬头快照。
// 用户撤掉当前抬头后，新开票付款仍需要重新配置，旧发票继续可下载。
// 删除范围由会话固定，不能提交他人ID或按税号批量删除。
func DeleteInvoiceProfile(c *gin.Context) {
	if err := model.DB.Where("user_id = ?", c.GetInt("id")).Delete(&model.InvoiceProfile{}).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// 发票分页对用户接口强制传用户ID，对财务接口仅在路由已授权后传0。
// 关键词限长且参数化查询，不能把个人税务资料作为公开搜索索引。
func invoiceList(c *gin.Context, userID int) {
	c.Header("Cache-Control", "private, no-store")                                       // 列表包含冻结抬头，同样属于私有税务数据。
	page, size := common.GetPageQuery(c).GetPage(), common.GetPageQuery(c).GetPageSize() // 复用现有分页边界。
	if requested, err := strconv.Atoi(c.Query("size")); err == nil && requested > 0 {
		size = min(requested, 100) // 不允许一次读取无界数量的私有抬头快照。
	}
	keyword := strings.TrimSpace(c.Query("keyword")) // 搜索只匹配申请订单号和发票号。
	if len(keyword) > 100 {
		common.ApiErrorT(c, "Search keyword is too long")
		return
	}
	status := strings.TrimSpace(c.Query("status")) // 不接受任意业务状态写进筛选契约。
	if status != "" && status != "pending_payment" && status != "pending" && status != "processing" && status != "issued" && status != "rejected" {
		common.ApiErrorT(c, "Invalid invoice status")
		return
	}
	records, total, err := model.InvoiceRecords(userID, page, size, status, keyword) // 统一从已冻结申请读取金额。
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": records, "total": total, "page": page, "size": size})
}

// 本人列表与财务列表共享分页实现，不能以请求参数选择用户范围。
// 财务入口只有在FinanceAuth及finance:read均通过之后才会执行。
func GetUserInvoices(c *gin.Context)    { invoiceList(c, c.GetInt("id")) }
func GetFinanceInvoices(c *gin.Context) { invoiceList(c, 0) }

// InvoiceStatusRequest只承载人工处理字段，不开放税务快照或金额修改。
type InvoiceStatusRequest struct {
	Status        string `json:"status"`         // 仅允许待处理之后的人工流转。
	InvoiceNumber string `json:"invoice_number"` // 发票号码不等同于支付订单号。
	Remark        string `json:"remark"`         // 拒绝时必填，解释费用需要人工处理。
}

// UpdateInvoiceStatus只由finance:write路由调用，模型仍校验付款与状态边界。
// 拒绝申请不代表退还附加费，退款必须通过单独的真实收款核对流程。
func UpdateInvoiceStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id")) // 申请身份只能来自路由，不接受任意查询条件。
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var req InvoiceStatusRequest // 请求结构没有付款、金额和抬头覆盖字段。
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	result, err := model.UpdateInvoiceRequest(id, model.InvoiceProcessing{Status: req.Status, InvoiceNumber: req.InvoiceNumber, Remark: req.Remark})
	if errors.Is(err, gorm.ErrRecordNotFound) { // 查不到记录统一返回可理解的业务失败。
		common.ApiErrorT(c, "Invoice request not found")
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result) // 只在数据库确认保存后返回更新资料。
}
