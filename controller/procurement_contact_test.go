package controller

import (
	"bytes"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 采购联系人通过真实控制器入口验证，不复制生产校验逻辑。
// 保存接口、数据库值、内存配置和匿名状态输出属于同一行为契约。
// 二维码生命周期另行通过 multipart 上传和真实图片读取覆盖。
// 所有外部输入采用固定样例，测试不依赖网络或随机图片。
// 使用临时 SQLite 只验证通用配置读写；没有新增表结构或驱动变更。
// 该测试不声明已完成 MySQL、PostgreSQL 的数据库兼容矩阵。
// 案例共享一次配置初始化，以便明确验证拒绝更新时原值保留。
// 测试必须串行执行，因为生产控制器读取进程级数据库与配置。
func TestProcurementContactOptions(t *testing.T) {
	// Gin 使用测试模式，响应由真实上下文和 Recorder 收集。
	gin.SetMode(gin.TestMode)
	// 内存数据库与开发环境隔离，不访问用户数据。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// 仅建立控制器及审计所需的既有模型，不新增业务 schema。
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.User{}, &model.Log{}, &model.AuditLog{}))
	// 保存全部被替换的全局依赖，清理时恢复调用前状态。
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalRedis := common.RedisEnabled
	// 禁用外部 Redis，使配置和审计路径只使用测试内存数据库。
	common.RedisEnabled = false
	model.LOG_DB = db
	// 替换配置映射遵守生产代码的锁契约。
	common.OptionMapRWMutex.Lock()
	originalOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	// 日志与配置指向同一测试库，避免审计写入真实日志库。
	model.DB = db
	// 注册清理优先于断言，测试中途失败也恢复全局依赖。
	// 恢复后再关闭底层连接，不把已关闭句柄留给其他测试。
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.RedisEnabled = originalRedis
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptions
		common.OptionMapRWMutex.Unlock()
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	// 表驱动覆盖合法地址、危险地址、长度边界与清空行为。
	// 预期值明确写出，不通过生产函数重新计算预期。
	for _, tc := range []struct {
		name, key, value, saved string
		valid                   bool
	}{
		// 姓名保存前去除两端空白，中文字符按字符数计数。
		{"contact name", "ProcurementContactName", "  采购支持  ", "采购支持", true},
		// 微信号同样归一化，复制按钮得到的值不含多余空格。
		{"wechat id", "ProcurementWeChatID", " lab_support ", "lab_support", true},
		// 外部 HTTPS 图片是允许的公开资源地址。
		{"https image", "ProcurementWeChatQRCodeURL", "https://example.org/qr.png", "https://example.org/qr.png", true},
		// 站内绝对路径支持部署时自有静态图片。
		{"site image", "ProcurementWeChatQRCodeURL", "/images/wechat.png", "/images/wechat.png", true},
		// 拒绝脚本协议，不能把可执行地址传到公开页面。
		{"javascript rejected", "ProcurementWeChatQRCodeURL", "javascript:alert(1)", "", false},
		// 双斜杠不能冒充站内路径绕过 HTTPS 要求。
		{"protocol relative rejected", "ProcurementWeChatQRCodeURL", "//example.org/qr.png", "", false},
		// 反斜杠可能被浏览器重新解释为路径分隔符。
		{"backslash rejected", "ProcurementWeChatQRCodeURL", "/\\example.org/qr.png", "", false},
		// 拒绝外部明文 HTTP 图片，避免与 HTTPS 页面混合。
		{"http rejected", "ProcurementWeChatQRCodeURL", "http://example.org/qr.png", "", false},
		// 地址不能含用户名或密码，以免公开图片暴露凭据。
		{"userinfo rejected", "ProcurementWeChatQRCodeURL", "https://user:pass@example.org/qr.png", "", false},
		// 超长 URL 必须在写数据库前失败。
		{"long url rejected", "ProcurementWeChatQRCodeURL", "https://example.org/" + strings.Repeat("a", 2048), "", false},
		// 中文姓名的 100 字符限制不按 UTF-8 字节数计算。
		{"long name rejected", "ProcurementContactName", strings.Repeat("名", 101), "", false},
		// 微信号与姓名使用相同的长度约束。
		{"long id rejected", "ProcurementWeChatID", strings.Repeat("a", 101), "", false},
		// 空地址是主动隐藏二维码的合法设置。
		{"clear qr", "ProcurementWeChatQRCodeURL", "", "", true},
	} {
		// 每个案例保存更新前值，用来确认失败没有破坏已有配置。
		t.Run(tc.name, func(t *testing.T) {
			common.OptionMapRWMutex.RLock()
			previous := common.OptionMap[tc.key]
			common.OptionMapRWMutex.RUnlock()
			// 请求使用通用 JSON 包装函数，与正式客户端协议一致。
			payload, err := common.Marshal(map[string]string{"key": tc.key, "value": tc.value})
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			// 模拟已通过路由鉴权的管理员身份，供真实审计逻辑使用。
			ctx.Set("role", common.RoleRootUser)
			ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(payload))
			// 调用真实保存入口，连同校验及持久化流程一起测试。
			UpdateOption(ctx)
			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			// 成功标志必须与每个输入的明确期望一致。
			require.Equal(t, tc.valid, response.Success, recorder.Body.String())
			// 失败路径保持原值，成功路径使用归一化后的预期值。
			expected := previous
			if tc.valid {
				expected = tc.saved
			}
			// 分别检查持久化与内存结果，防止只更新其中一层。
			var stored model.Option
			require.NoError(t, db.First(&stored, model.Option{Key: tc.key}).Error)
			// 数据库值是重新查询得到，避免只断言请求中的字符串。
			assert.Equal(t, expected, stored.Value)
			common.OptionMapRWMutex.RLock()
			actual := common.OptionMap[tc.key]
			common.OptionMapRWMutex.RUnlock()
			assert.Equal(t, expected, actual)
		})
	}
	// 公开采购字段必须与认证二维码配置相互独立。
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	// 匿名首页读取真实状态接口，不从测试配置直接组装响应。
	GetStatus(ctx)
	// 只声明本次契约需要的字段，不绑定整个状态响应快照。
	var status struct {
		Success bool           `json:"success"`
		Data    map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &status))
	require.True(t, status.Success)
	// 公开姓名和微信号必须来自各自采购设置。
	assert.Equal(t, "采购支持", status.Data["procurement_contact_name"])
	assert.Equal(t, "lab_support", status.Data["procurement_wechat_id"])
	assert.Equal(t, "", status.Data["procurement_wechat_qr_url"])
	// 采购信息不会替代原有微信登录二维码。
	assert.Equal(t, common.WeChatAccountQRCodeImageURL, status.Data["wechat_qrcode"])

	// 生命周期覆盖上传、持久化、公开读取、配置隔离和删除。
	// 同一张固定 PNG 贯穿所有步骤，验证内容未被意外重编码。
	t.Run("upload, serve and remove QR image", func(t *testing.T) {
		// 生成最小合法 PNG，不从仓库或网络读取图片文件。
		var imageBody bytes.Buffer
		require.NoError(t, png.Encode(&imageBody, image.NewRGBA(image.Rect(0, 0, 2, 2))))
		// 使用真实 multipart 编码，与浏览器表单上传保持一致。
		var requestBody bytes.Buffer
		writer := multipart.NewWriter(&requestBody)
		// 文件字段名是公开上传接口约定的 file。
		part, err := writer.CreateFormFile("file", "procurement-qr.png")
		require.NoError(t, err)
		// 检查写入错误，避免把构造失败误判为上传控制器失败。
		_, err = part.Write(imageBody.Bytes())
		require.NoError(t, err)
		// 关闭 writer 写入末尾边界，保证请求体完整。
		require.NoError(t, writer.Close())

		// 上传入口使用独立请求上下文，避免复用已消费的 Body。
		uploadRecorder := httptest.NewRecorder()
		uploadContext, _ := gin.CreateTestContext(uploadRecorder)
		// 上传审计只需要已授权角色，不引入真实用户会话或令牌。
		uploadContext.Set("role", common.RoleRootUser)
		uploadContext.Request = httptest.NewRequest(http.MethodPost, "/api/option/procurement-wechat-qr", &requestBody)
		uploadContext.Request.Header.Set("Content-Type", writer.FormDataContentType())
		// 测试真实图片验证和配置写入，不 mock 控制器实现。
		UploadProcurementQRCode(uploadContext)
		// 成功响应只需公开 URL，内部存储编码不是客户端契约。
		var uploadResponse struct {
			Success bool `json:"success"`
			Data    struct {
				URL string `json:"url"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(uploadRecorder.Body.Bytes(), &uploadResponse))
		require.True(t, uploadResponse.Success, uploadRecorder.Body.String())
		// 版本查询参数使替换图片后浏览器使用新缓存地址。
		assert.Contains(t, uploadResponse.Data.URL, procurementQRCodePath+"?v=")

		// 图片编码持久化在专用 option，与公开 URL 分开存储。
		var imageOption model.Option
		require.NoError(t, db.First(&imageOption, model.Option{Key: procurementQRCodeDataOption}).Error)
		assert.NotEmpty(t, imageOption.Value)
		// 公开 URL 必须和上传成功响应完全一致。
		var urlOption model.Option
		require.NoError(t, db.First(&urlOption, model.Option{Key: "ProcurementWeChatQRCodeURL"}).Error)
		assert.Equal(t, uploadResponse.Data.URL, urlOption.Value)

		// 使用上传返回的实际 URL 请求公开图片。
		imageRecorder := httptest.NewRecorder()
		imageContext, _ := gin.CreateTestContext(imageRecorder)
		imageContext.Request = httptest.NewRequest(http.MethodGet, uploadResponse.Data.URL, nil)
		// 公开读取必须返回上传图片，而不是编码字符串或占位内容。
		GetProcurementQRCode(imageContext)
		assert.Equal(t, http.StatusOK, imageRecorder.Code)
		// 响应 MIME 由图片内容决定，不能依赖上传扩展名。
		assert.Equal(t, "image/png", imageRecorder.Header().Get("Content-Type"))
		// 二维码字节完整保留，保证扫描所需的图像质量。
		assert.Equal(t, imageBody.Bytes(), imageRecorder.Body.Bytes())
		// 显式禁止浏览器把图片响应猜测成其他内容类型。
		assert.Equal(t, "nosniff", imageRecorder.Header().Get("X-Content-Type-Options"))

		// 读取通用配置列表，确认内部图片编码不会被额外暴露。
		optionsRecorder := httptest.NewRecorder()
		optionsContext, _ := gin.CreateTestContext(optionsRecorder)
		optionsContext.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)
		GetOptions(optionsContext)
		// 同时检查键和值，避免仅改字段名后仍泄露大段图片内容。
		assert.NotContains(t, optionsRecorder.Body.String(), procurementQRCodeDataOption)
		assert.NotContains(t, optionsRecorder.Body.String(), imageOption.Value)

		// 删除使用专用入口，不通过绕过业务的直接数据库清空。
		deleteRecorder := httptest.NewRecorder()
		deleteContext, _ := gin.CreateTestContext(deleteRecorder)
		// 删除同样由管理员执行，公开读取请求则保持匿名。
		deleteContext.Set("role", common.RoleRootUser)
		deleteContext.Request = httptest.NewRequest(http.MethodDelete, "/api/option/procurement-wechat-qr", nil)
		// 删除必须同时移除图片及公开地址。
		DeleteProcurementQRCode(deleteContext)
		var deleteResponse struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(deleteRecorder.Body.Bytes(), &deleteResponse))
		// 只有实际成功的删除才允许前端隐藏图片预览。
		require.True(t, deleteResponse.Success, deleteRecorder.Body.String())

		// 删除后重新访问公开 URL，验证服务端真实不可用。
		missingRecorder := httptest.NewRecorder()
		missingContext, _ := gin.CreateTestContext(missingRecorder)
		missingContext.Request = httptest.NewRequest(http.MethodGet, procurementQRCodePath, nil)
		GetProcurementQRCode(missingContext)
		// 缺失图片以 404 表达，不返回成功的空响应体。
		assert.Equal(t, http.StatusNotFound, missingContext.Writer.Status())
	})

	// 宽度超限的图片仍是合法 PNG，不能靠格式拒绝替代尺寸校验。
	// 只生成一行像素，避免边界测试本身占用大量内存。
	var wideImage bytes.Buffer
	require.NoError(t, png.Encode(&wideImage, image.NewRGBA(image.Rect(0, 0, 4097, 1))))
	// 所有失败案例沿用刚完成删除的空配置作为明确基线。
	// 不向数据库预写坏数据，失败状态必须来自真实 HTTP 输入。
	// 每个案例独立构造请求体，不复用已被消费的 Reader。
	// 文件内容、文件体积与解码尺寸是三个独立的上传约束。
	// 所有请求都直接调用服务端，不依赖浏览器文件选择器。
	for _, tc := range []struct {
		name string
		data []byte
	}{
		// 扩展名是 PNG，但正文不是图片。
		{"invalid image content", []byte("not an image")},
		// 请求仍小于 multipart 总上限，确保触发文件本身的大小检查。
		{"oversized image", bytes.Repeat([]byte("x"), (1<<20)+1)},
		// 压缩后很小的图片也不能绕过解码尺寸约束。
		{"oversized dimensions", wideImage.Bytes()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 使用真实 multipart 编码，与浏览器表单上传保持一致。
			var requestBody bytes.Buffer
			writer := multipart.NewWriter(&requestBody)
			// 字段名 file 与浏览器的上传 API 保持一致。
			part, err := writer.CreateFormFile("file", "fake.png")
			require.NoError(t, err)
			_, err = part.Write(tc.data)
			require.NoError(t, err)
			// 完整关闭边界，排除因损坏 multipart 导致的假阳性。
			require.NoError(t, writer.Close())

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/option/procurement-wechat-qr", &requestBody)
			ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
			// 校验必须在写入图片配置之前完成。
			UploadProcurementQRCode(ctx)
			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			// 失败不能伪装成成功，也不能重新发布已被删除的二维码。
			assert.False(t, response.Success)
			common.OptionMapRWMutex.RLock()
			assert.Empty(t, common.OptionMap[procurementQRCodeDataOption])
			assert.Empty(t, common.OptionMap["ProcurementWeChatQRCodeURL"])
			common.OptionMapRWMutex.RUnlock()
		})
	}
}
