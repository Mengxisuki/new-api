package service

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
)

// 错误类别区分配置、网络、协议和签名阶段，不包含商户密钥原文。
var (
	ErrOfficialPaymentConfig    = errors.New("official payment config invalid")
	ErrOfficialPaymentRequest   = errors.New("official payment request failed")
	ErrOfficialPaymentResponse  = errors.New("official payment response invalid")
	ErrOfficialPaymentSignature = errors.New("official payment signature invalid")
)

// 上游请求与通知解析统一使用短超时，调用方取消会提前终止。
const officialPaymentTimeout = 15 * time.Second

type OfficialPaymentResult struct {
	RedirectURL string // 用户浏览器跳转地址，不是付款成功凭证。
	QRCode      string // 原始扫码内容，前端本地绘制二维码。
}

type OfficialPaymentCallback struct {
	TradeNo     string          // 平台订单号，结算只按本地持久化记录认领。
	ProviderRef string          // 上游交易引用仅供审计定位。
	Status      string          // 保留上游状态，控制器明确判断成功枚举。
	Amount      decimal.Decimal // 单位为元，来自通知原始总金额。
	Currency    string          // 币种不可由控制器用默认值掩盖缺失。
}

// 统一官方渠道下单入口，只接受已保存并通过服务端校验的渠道。
// 充值和订阅共用此服务，具体权益由各自的订单完成事务发放。
// 输入金额使用十进制定点数，调用者须传入订单保存的最终收款金额。
// 禁止客户端传入商户配置，防止攻击者替换收款账户或平台公钥。
// 交易号是本平台订单主键，不能使用模型厂商的账户标识替代。
// 回调地址属于平台收款链路，不应包含用户密钥或会话信息。
// 返回值仅表示上游提供了付款入口，不代表用户已经付款。
// 二维码内容和跳转地址必须经过交互类型分支，由前端对应展示。
// 微信下单会在同一请求中校验商户签名与平台响应签名。
// 支付宝页面支付只构建签名入口，到账仍必须等待异步签名通知。
// 所有上游HTTP请求都有明确超时，调用取消向下传播。
// 未知渠道类型直接失败，不能静默切换到另一家支付网关。
func CreateOfficialPayment(ctx context.Context, channel operation_setting.OfficialPaymentChannel, tradeNo string, amount decimal.Decimal, subject string, clientIP string, notifyURL string, returnURL string, expiresAt time.Time) (*OfficialPaymentResult, error) {
	// 完整配置先验证，再创建任何签名或外部请求。
	if err := operation_setting.ValidateOfficialPaymentChannel(channel); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOfficialPaymentConfig, err)
	}
	if strings.TrimSpace(tradeNo) == "" || amount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("%w: invalid order number or amount", ErrOfficialPaymentConfig)
	}

	// 类型分派不接受隐式默认网关。
	switch channel.ChannelType {
	case operation_setting.PaymentChannelAlipay:
		return createOfficialAlipayPayment(ctx, channel, tradeNo, amount, subject, notifyURL, returnURL, expiresAt)
	case operation_setting.PaymentChannelWechat:
		return createOfficialWechatPayment(ctx, channel, tradeNo, amount, subject, clientIP, notifyURL, returnURL, expiresAt)
	default:
		return nil, fmt.Errorf("%w: unsupported channel type", ErrOfficialPaymentConfig)
	}
}

// 关闭上游未支付订单，供继续支付流程在替换本地订单前调用。
// 必须先确认原单已关闭，才能允许创建新单，防止双重付款。
// 关闭失败保留旧订单，本地事务不能伪造上游关单成功。
// 已付款订单可能无法关闭，调用者应提示用户刷新账单确认。
// 关闭接口复用下单时商户身份，不能由请求体覆盖商户号。
// 空订单号在发起网络请求前拒绝，避免产生无意义签名流量。
// 调用者使用用户归属查询取得订单，不直接信任客户端交易号。
// 渠道停用后是否允许重试由控制器决定，服务仍验证配置完整性。
// 微信关单响应同样需要平台验签，不只检查HTTP状态。
// 请求取消和超时会返回错误，外层不得继续替换订单。
// 此函数不修改数据库状态，也不发放或退还平台额度。
func CloseOfficialPayment(ctx context.Context, channel operation_setting.OfficialPaymentChannel, tradeNo string) error {
	// 完整配置先验证，再创建任何签名或外部请求。
	if err := operation_setting.ValidateOfficialPaymentChannel(channel); err != nil {
		return fmt.Errorf("%w: %v", ErrOfficialPaymentConfig, err)
	}
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return fmt.Errorf("%w: invalid order number", ErrOfficialPaymentConfig)
	}
	// 类型分派不接受隐式默认网关。
	switch channel.ChannelType {
	case operation_setting.PaymentChannelAlipay:
		return closeOfficialAlipayPayment(ctx, channel, tradeNo)
	case operation_setting.PaymentChannelWechat:
		return closeOfficialWechatPayment(ctx, channel, tradeNo)
	default:
		return fmt.Errorf("%w: unsupported channel type", ErrOfficialPaymentConfig)
	}
}

// 验证支付宝异步通知，验签覆盖原始表单中的全部签名字段。
// 先检查渠道类型，避免把微信配置用于支付宝验证。
// 签名算法必须是受支持的RSA系列，未知算法不能降级接受。
// 应用ID必须与当前渠道相同，签名合法不等于订单属于本应用。
// 回调公钥使用运营侧配置，用户无法通过通知提交替代密钥。
// 签名值先做Base64解码，解析失败与验签失败都会阻止到账。
// 签名串按支付宝规则排序并排除sign和sign_type字段。
// RSA2使用SHA256，旧RSA模式保留给原渠道配置的真实调用。
// 金额以十进制解析，拒绝零、负数及非金额文本。
// 只输出结算需要的订单号、交易号、状态和金额。
// 不将完整回调原文写入日志，避免记录付款人资料。
// 成功验签仍需由控制器检查交易成功状态和币种。
// 最终订单金额、渠道和幂等状态必须在数据库事务内再次匹配。
// 验证过程不访问用户钱包，也不会直接创建订阅权益。
func VerifyOfficialAlipayCallback(channel operation_setting.OfficialPaymentChannel, form url.Values) (*OfficialPaymentCallback, error) {
	if channel.ChannelType != operation_setting.PaymentChannelAlipay {
		return nil, fmt.Errorf("%w: channel type mismatch", ErrOfficialPaymentConfig)
	}
	// 通知签名必须显式存在，不能把缺失当作无需验签。
	sign := strings.TrimSpace(form.Get("sign"))
	if sign == "" {
		return nil, fmt.Errorf("%w: sign is required", ErrOfficialPaymentSignature)
	}
	// 算法标识来自通知；缺失时才采用已保存的商户算法约定。
	signType := strings.ToUpper(strings.TrimSpace(form.Get("sign_type")))
	if signType == "" {
		signType = strings.ToUpper(strings.TrimSpace(channel.Config["sign_type"]))
	}
	if signType != "RSA2" && signType != "RSA" {
		return nil, fmt.Errorf("%w: invalid sign type", ErrOfficialPaymentSignature)
	}
	if !strings.EqualFold(strings.TrimSpace(form.Get("app_id")), strings.TrimSpace(channel.Config["app_id"])) {
		return nil, fmt.Errorf("%w: app_id mismatch", ErrOfficialPaymentSignature)
	}

	// 使用后台保存的平台公钥，不接受通知参数提供的新公钥。
	publicKey, err := parseOfficialPaymentPublicKey(channel.Config["alipay_public_key"])
	if err != nil {
		return nil, err
	}
	signBytes, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base64 signature", ErrOfficialPaymentSignature)
	}
	// 验签内容使用表单解码后的原字段，不经过业务格式化。
	content := buildAlipayCallbackSignContent(form)
	var digest []byte
	var hash crypto.Hash
	// 两种显式支持算法各用对应摘要，不能互相试探绕过。
	if signType == "RSA" {
		sum := sha1.Sum([]byte(content))
		digest = sum[:]
		hash = crypto.SHA1
	} else {
		sum := sha256.Sum256([]byte(content))
		digest = sum[:]
		hash = crypto.SHA256
	}
	// RSA验签失败直接阻止后续金额和订单解析。
	if err := rsa.VerifyPKCS1v15(publicKey, hash, digest, signBytes); err != nil {
		return nil, fmt.Errorf("%w: verification failed", ErrOfficialPaymentSignature)
	}

	// 实收金额按十进制解析，后续与订单冻结金额核对。
	amount, err := decimal.NewFromString(strings.TrimSpace(form.Get("total_amount")))
	if err != nil || amount.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("%w: invalid total_amount", ErrOfficialPaymentResponse)
	}

	return &OfficialPaymentCallback{
		TradeNo:     strings.TrimSpace(form.Get("out_trade_no")),
		ProviderRef: strings.TrimSpace(form.Get("trade_no")),
		Status:      strings.ToUpper(strings.TrimSpace(form.Get("trade_status"))),
		Amount:      amount,
		Currency:    "CNY",
	}, nil
}

// 验证微信支付APIv3通知并用APIv3密钥解密交易资源。
// HTTP头中的平台公钥ID必须与配置公钥匹配，不能信任通知自带密钥。
// SDK先验证原始报文签名，再解密资源；两步顺序不能颠倒。
// 解密只产生结构化交易，不持久化付款人信息和原始密文。
// 空报文在初始化通知解析器前拒绝，避免把缺失交易当作成功。
// 通知处理使用请求上下文及超时，不引入后台无限等待。
// 构造的HTTPS地址只供SDK解析，不会发出外部网络请求。
// 原始请求头完整复制，保留签名覆盖的时间戳和随机串。
// 订单号、交易状态与总金额缺失均视为无效通知。
// 同一微信平台公钥可以服务多个商户，因此还要核对商户号与应用ID。
// 交易金额以分为整数，转换为元时使用decimal避免二进制误差。
// 币种必须由通知明确提供，不能默认为人民币掩盖坏数据。
// 非成功交易的处理由控制器决定，本层不将状态重写成成功。
// 返回的上游交易号用于审计定位，不含用户支付凭证。
// 验签失败与商户不匹配均返回签名错误类别，阻止权益变更。
// 本函数不操作数据库，也不因重复通知本身返回付款成功。
func VerifyOfficialWechatCallback(ctx context.Context, channel operation_setting.OfficialPaymentChannel, headers http.Header, body []byte) (*OfficialPaymentCallback, error) {
	if channel.ChannelType != operation_setting.PaymentChannelWechat {
		return nil, fmt.Errorf("%w: channel type mismatch", ErrOfficialPaymentConfig)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("%w: empty callback body", ErrOfficialPaymentResponse)
	}
	// 固定平台公钥模式不依赖外网下载证书。
	publicKey, err := parseOfficialPaymentPublicKey(channel.Config["wechatpay_public_key"])
	if err != nil {
		return nil, err
	}

	// 网络与通知处理都有上限，避免上游故障长期占用工作线程。
	ctx, cancel := context.WithTimeout(ctx, officialPaymentTimeout)
	defer cancel() // 请求完成后释放上下文计时器。
	// ID与公钥一起选择验签身份，防止只按可伪造头部信任报文。
	verifier := verifiers.NewSHA256WithRSAPubkeyVerifier(channel.Config["wechatpay_public_key_id"], *publicKey)
	// SDK按APIv3协议完成验签和AEAD解密，避免自研重复密码逻辑。
	handler, err := notify.NewRSANotifyHandler(channel.Config["api_v3_key"], verifier)
	if err != nil {
		return nil, fmt.Errorf("%w: initialize notify handler: %v", ErrOfficialPaymentConfig, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://notify.wechat.example/callback", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: build callback request", ErrOfficialPaymentResponse)
	}
	req.Header = headers.Clone()             // 保留SDK验签使用的所有原始HTTP头。
	transaction := new(payments.Transaction) // 仅解析到账需要的结构化交易。
	// SDK必须先确认签名再解密资源，未可信正文不进入业务判断。
	_, err = handler.ParseNotifyRequest(ctx, req, transaction)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOfficialPaymentSignature, err)
	}
	if transaction.OutTradeNo == nil || transaction.TradeState == nil || transaction.Amount == nil || transaction.Amount.Total == nil {
		return nil, fmt.Errorf("%w: incomplete transaction", ErrOfficialPaymentResponse)
	}

	// 验签证明消息来源，商户和应用匹配则证明交易属于当前渠道。
	// 同一平台公钥可服务多个商户，不能只验证签名后直接认领订单。
	if transaction.Appid == nil || transaction.Mchid == nil ||
		strings.TrimSpace(*transaction.Appid) != channel.Config["appid"] ||
		strings.TrimSpace(*transaction.Mchid) != channel.Config["mchid"] {
		return nil, fmt.Errorf("%w: merchant or app mismatch", ErrOfficialPaymentSignature)
	}
	if *transaction.Amount.Total <= 0 {
		return nil, fmt.Errorf("%w: invalid transaction amount", ErrOfficialPaymentResponse)
	}

	if transaction.Amount.Currency == nil || strings.TrimSpace(*transaction.Amount.Currency) == "" {
		return nil, fmt.Errorf("%w: missing transaction currency", ErrOfficialPaymentResponse)
	}
	currency := strings.ToUpper(strings.TrimSpace(*transaction.Amount.Currency))
	providerRef := "" // 上游交易号缺失不等于本平台订单号缺失。
	if transaction.TransactionId != nil {
		providerRef = strings.TrimSpace(*transaction.TransactionId)
	}
	return &OfficialPaymentCallback{
		TradeNo:     strings.TrimSpace(*transaction.OutTradeNo),
		ProviderRef: providerRef,
		Status:      strings.ToUpper(strings.TrimSpace(*transaction.TradeState)),
		Amount:      decimal.NewFromInt(*transaction.Amount.Total).Div(decimal.NewFromInt(100)),
		Currency:    currency,
	}, nil
}

// 根据支付宝渠道交互方式生成预下单二维码或浏览器支付入口。
// 配置中的通知地址可覆盖部署默认地址，供既有商户后台对接。
// 页面回跳只用于展示结果，不能作为付款到账凭据。
// 扫码模式使用当面付预创建接口，页面与WAP使用各自产品码。
// 产品码和API方法成对选择，避免把不同交互误发到同一接口。
// 订单金额格式化到两位小数，与平台订单保存的金额一致。
// 订单标题只描述所购服务，不包含个人资料或API密钥。
// 支付时限固定为十分钟，用户超过窗口后应重新创建付款入口。
// 业务参数先编码为JSON，再作为签名字段，不能重新排序已签内容。
// 证书序列号配置仅随签名请求发送，不用于跳过平台公钥校验。
// 签名失败即终止，不返回一个看似可用的未签名付款地址。
// 页面模式把签名参数编码为查询字符串，保留URL原有参数。
// 二维码模式向渠道网关发送表单，并声明期望JSON响应。
// 请求受上下文超时控制，HTTP失败不会生成伪二维码。
// 非2xx响应视为上游失败，不把响应正文当作付款链接。
// 成功响应必须含正确业务码与非空二维码。
// 上游错误只作为失败原因返回，不能静默改走页面模式。
// 此层不确认到账；即使拿到二维码，也仍是待支付订单。
func createOfficialAlipayPayment(ctx context.Context, channel operation_setting.OfficialPaymentChannel, tradeNo string, amount decimal.Decimal, subject string, notifyURL string, returnURL string, expiresAt time.Time) (*OfficialPaymentResult, error) {
	config := channel.Config // 调用者已经取得独立渠道快照。
	if strings.TrimSpace(config["notify_url"]) != "" {
		notifyURL = config["notify_url"]
	}
	if strings.TrimSpace(config["return_url"]) != "" {
		returnURL = config["return_url"]
	}
	mode := channel.InteractionMode // 不按设备UA暗中改变管理员配置的交互。
	method := ""
	productCode := ""
	switch mode {
	case operation_setting.PaymentInteractionQR:
		method = "alipay.trade.precreate"
		productCode = "FACE_TO_FACE_PAYMENT"
	case operation_setting.PaymentInteractionWAP:
		method = "alipay.trade.wap.pay"
		productCode = "QUICK_WAP_WAY"
	case operation_setting.PaymentInteractionPage:
		method = "alipay.trade.page.pay"
		productCode = "FAST_INSTANT_TRADE_PAY"
	default:
		return nil, fmt.Errorf("%w: unsupported Alipay interaction mode", ErrOfficialPaymentConfig)
	}

	// 业务JSON先稳定编码，再作为签名字段进入请求。
	bizContent, err := common.Marshal(map[string]any{
		"out_trade_no":    tradeNo,
		"total_amount":    amount.Round(2).StringFixed(2),
		"subject":         strings.TrimSpace(subject),
		"product_code":    productCode,
		"timeout_express": "10m", // 上游付款窗口与平台倒计时约定一致。
	})
	if err != nil {
		return nil, fmt.Errorf("%w: encode biz_content", ErrOfficialPaymentConfig)
	}
	params := map[string]string{
		"app_id":      config["app_id"],
		"method":      method,
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   strings.ToUpper(config["sign_type"]),
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"notify_url":  notifyURL,
		"biz_content": string(bizContent),
	}
	if mode != operation_setting.PaymentInteractionQR {
		params["return_url"] = returnURL
	}
	if config["app_cert_sn"] != "" {
		params["app_cert_sn"] = config["app_cert_sn"]
	}
	if config["alipay_root_cert_sn"] != "" {
		params["alipay_root_cert_sn"] = config["alipay_root_cert_sn"]
	}
	sign, err := signOfficialAlipayContent(buildAlipaySignContent(params), config["private_key"], params["sign_type"])
	if err != nil {
		return nil, err
	}
	params["sign"] = sign // 完成签名后不可再修改参与签名的参数。

	if mode != operation_setting.PaymentInteractionQR {
		gatewayURL, err := url.Parse(config["gateway_url"])
		if err != nil {
			return nil, fmt.Errorf("%w: invalid gateway URL", ErrOfficialPaymentConfig)
		}
		query := gatewayURL.Query() // 保留已有网关查询参数并正确编码新增字段。
		for key, value := range params {
			query.Set(key, value)
		}
		gatewayURL.RawQuery = query.Encode()
		return &OfficialPaymentResult{RedirectURL: gatewayURL.String()}, nil
	}

	form := url.Values{} // 网络传输编码与签名原文编码分别处理。
	for key, value := range params {
		form.Set(key, value)
	}
	// 网络与通知处理都有上限，避免上游故障长期占用工作线程。
	ctx, cancel := context.WithTimeout(ctx, officialPaymentTimeout)
	defer cancel() // 请求完成后释放上下文计时器。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config["gateway_url"], strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%w: build Alipay request", ErrOfficialPaymentRequest)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded") // 支付宝API使用表单协议。
	req.Header.Set("Accept", "application/json")
	// 请求上下文控制超时，网络错误直接返回给订单控制器。
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOfficialPaymentRequest, err)
	}
	defer resp.Body.Close() // 所有读取和业务解析分支都能释放HTTP响应。
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read Alipay response", ErrOfficialPaymentResponse)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: Alipay returned HTTP %d", ErrOfficialPaymentResponse, resp.StatusCode)
	}
	var response map[string]any // 响应只用于读取明确协议节点，不直接透传给用户。
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("%w: decode Alipay response", ErrOfficialPaymentResponse)
	}
	node, ok := response["alipay_trade_precreate_response"].(map[string]any)
	if !ok || strings.TrimSpace(paymentString(node["code"])) != "10000" {
		message := strings.TrimSpace(paymentString(node["sub_msg"]))
		if message == "" {
			message = strings.TrimSpace(paymentString(node["msg"]))
		}
		return nil, fmt.Errorf("%w: %s", ErrOfficialPaymentResponse, message)
	}
	// 返回码成功也必须存在可扫码内容，不能用空字符串伪造可付款。
	qrCode := strings.TrimSpace(paymentString(node["qr_code"]))
	if qrCode == "" {
		return nil, fmt.Errorf("%w: missing qr_code", ErrOfficialPaymentResponse)
	}
	return &OfficialPaymentResult{QRCode: qrCode}, nil
}

// 支付宝关单使用与下单相同的商户签名身份。
// 只发送平台订单号，不接受客户端传入另一个上游交易号。
// 关单业务参数通过公共JSON封装生成，保持项目统一编码行为。
// 请求方法明确为交易关闭，不能复用支付页面跳转URL。
// 签名覆盖应用、方法、时间戳和业务参数，防止参数被替换。
// 关单请求设置超时并传播调用取消，外层等待结果后再替换订单。
// 响应必须是2xx且含成功业务码，网络成功不等于关单成功。
// 支付宝返回失败时保留错误，不能把“已付款”当作可重试状态。
// 关闭响应体在解析完成后释放，避免耗尽HTTP连接。
// 这里只处理上游操作，本地旧单状态由后续事务原子更新。
// 本层不会把未支付订单标成已到账，也不会补发额度。
// 缺失响应节点属于协议错误，不能当作没有错误信息而放行。
func closeOfficialAlipayPayment(ctx context.Context, channel operation_setting.OfficialPaymentChannel, tradeNo string) error {
	config := channel.Config // 调用者已经取得独立渠道快照。
	bizContent, err := common.Marshal(map[string]string{"out_trade_no": tradeNo})
	if err != nil {
		return fmt.Errorf("%w: encode biz_content", ErrOfficialPaymentConfig)
	}
	params := map[string]string{
		"app_id":      config["app_id"],
		"method":      "alipay.trade.close",
		"format":      "JSON",
		"charset":     "utf-8",
		"sign_type":   strings.ToUpper(config["sign_type"]),
		"timestamp":   time.Now().Format("2006-01-02 15:04:05"),
		"version":     "1.0",
		"biz_content": string(bizContent),
	}
	sign, err := signOfficialAlipayContent(buildAlipaySignContent(params), config["private_key"], params["sign_type"])
	if err != nil {
		return err
	}
	params["sign"] = sign // 完成签名后不可再修改参与签名的参数。
	form := url.Values{}  // 网络传输编码与签名原文编码分别处理。
	for key, value := range params {
		form.Set(key, value)
	}
	// 网络与通知处理都有上限，避免上游故障长期占用工作线程。
	ctx, cancel := context.WithTimeout(ctx, officialPaymentTimeout)
	defer cancel() // 请求完成后释放上下文计时器。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config["gateway_url"], strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: build Alipay close request", ErrOfficialPaymentRequest)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded") // 支付宝API使用表单协议。
	// 请求上下文控制超时，网络错误直接返回给订单控制器。
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOfficialPaymentRequest, err)
	}
	defer resp.Body.Close() // 所有读取和业务解析分支都能释放HTTP响应。
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: close order failed", ErrOfficialPaymentResponse)
	}
	var response map[string]any // 响应只用于读取明确协议节点，不直接透传给用户。
	if err := common.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("%w: decode Alipay close response", ErrOfficialPaymentResponse)
	}
	node, ok := response["alipay_trade_close_response"].(map[string]any)
	if !ok || strings.TrimSpace(paymentString(node["code"])) != "10000" {
		return fmt.Errorf("%w: %s", ErrOfficialPaymentResponse, strings.TrimSpace(paymentString(node["sub_msg"])))
	}
	return nil
}

// 微信支付以商户私钥签名下单，并以配置平台公钥验证响应。
// 接口固定为官方域名，不能让用户任意指定微信API地址。
// 渠道可配置通知地址与H5回跳地址，均由管理员维护。
// 金额从元转换为分前检查整数精度，禁止截断小数分。
// 金额必须为正，下游SDK不得替业务逻辑猜测合法金额。
// 付款人IP来自服务端请求上下文，非法IP直接报告配置错误。
// Native与H5在上游使用不同接口，不通过二维码内容猜测类型。
// Native返回code_url供微信扫码，不作为浏览器直接跳转目标。
// H5请求需要scene_info及设备类型，保留原渠道的移动端配置。
// H5应用名称和地址仅在配置存在时发送，不制造空字段。
// 订单有效期使用RFC3339格式，避免时区信息丢失。
// 下单结果由SDK先验签，业务代码再检查响应结构。
// 响应缺失、HTTP错误或JSON解析失败都必须向调用者暴露。
// 二维码模式必须取得code_url，H5模式必须取得h5_url。
// H5回跳通过URL编码追加，不能拼接未转义的查询参数。
// 付款入口返回后订单仍未完成，必须等待官方异步通知。
// 服务端不从下单响应推断付款成功，不提前增加钱包或订阅。
func createOfficialWechatPayment(ctx context.Context, channel operation_setting.OfficialPaymentChannel, tradeNo string, amount decimal.Decimal, subject string, clientIP string, notifyURL string, returnURL string, expiresAt time.Time) (*OfficialPaymentResult, error) {
	config := channel.Config // 调用者已经取得独立渠道快照。
	if strings.TrimSpace(config["notify_url"]) != "" {
		notifyURL = config["notify_url"]
	}
	if strings.TrimSpace(config["h5_redirect_url"]) != "" {
		returnURL = config["h5_redirect_url"]
	}
	// 网络与通知处理都有上限，避免上游故障长期占用工作线程。
	ctx, cancel := context.WithTimeout(ctx, officialPaymentTimeout)
	defer cancel() // 请求完成后释放上下文计时器。
	// 请求签名与响应验签共用已配置的平台公钥模式。
	client, err := newOfficialWechatClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("%w: initialize WeChat Pay client", ErrOfficialPaymentConfig)
	}

	amountFen := amount.Mul(decimal.NewFromInt(100)) // 微信总金额单位固定为分。
	// IntPart之前限制整数分上界，超大配置不得溢出成负付款金额。
	if !amountFen.Equal(amountFen.Truncate(0)) || amountFen.LessThanOrEqual(decimal.Zero) || amountFen.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return nil, fmt.Errorf("%w: amount is outside supported fen precision or range", ErrOfficialPaymentConfig)
	}
	// 不把解析失败IP伪装成回环地址，以免风控信息与真实请求不一致。
	payerIP, err := normalizeOfficialPaymentIP(clientIP)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"appid":        config["appid"],
		"mchid":        config["mchid"],
		"description":  strings.TrimSpace(subject),
		"out_trade_no": tradeNo,
		"notify_url":   notifyURL,
		"time_expire":  expiresAt.Format(time.RFC3339),
		"amount": map[string]any{
			"total":    amountFen.IntPart(),
			"currency": "CNY",
		},
	}
	endpoint := "" // 仅下面两个明确支持的官方交易接口可用。
	if channel.InteractionMode == operation_setting.PaymentInteractionQR {
		endpoint = "/v3/pay/transactions/native"
		payload["scene_info"] = map[string]any{"payer_client_ip": payerIP} // Native风控来源保留真实客户端IP。
	} else {
		endpoint = "/v3/pay/transactions/h5"
		h5Info := map[string]any{"type": config["h5_type"]} // 类型已由服务端配置白名单验证。
		if config["h5_wap_url"] != "" {
			h5Info["app_url"] = config["h5_wap_url"]
		}
		if config["h5_wap_name"] != "" {
			h5Info["app_name"] = config["h5_wap_name"]
		}
		payload["scene_info"] = map[string]any{
			"payer_client_ip": payerIP,
			"h5_info":         h5Info,
		}
	}
	// 官方域名固定，商户配置不能将带签名请求发给任意主机。
	result, err := client.Post(ctx, "https://api.mch.weixin.qq.com"+endpoint, payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOfficialPaymentRequest, err)
	}
	if result == nil || result.Response == nil || result.Response.Body == nil {
		return nil, fmt.Errorf("%w: empty WeChat Pay response", ErrOfficialPaymentResponse)
	}
	defer result.Response.Body.Close() // 解析失败也必须归还连接。
	body, err := io.ReadAll(result.Response.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read WeChat Pay response", ErrOfficialPaymentResponse)
	}
	if result.Response.StatusCode < 200 || result.Response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: WeChat Pay returned HTTP %d: %s", ErrOfficialPaymentResponse, result.Response.StatusCode, strings.TrimSpace(string(body)))
	}
	var response map[string]any // 响应只用于读取明确协议节点，不直接透传给用户。
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("%w: decode WeChat Pay response", ErrOfficialPaymentResponse)
	}
	if channel.InteractionMode == operation_setting.PaymentInteractionQR {
		qrCode := strings.TrimSpace(paymentString(response["code_url"]))
		if qrCode == "" {
			return nil, fmt.Errorf("%w: missing code_url", ErrOfficialPaymentResponse)
		}
		return &OfficialPaymentResult{QRCode: qrCode}, nil
	}
	// H5模式必须返回实际跳转地址，不复用Native二维码字段。
	h5URL := strings.TrimSpace(paymentString(response["h5_url"]))
	if h5URL == "" {
		return nil, fmt.Errorf("%w: missing h5_url", ErrOfficialPaymentResponse)
	}
	parsed, err := url.Parse(h5URL)
	if err == nil && strings.TrimSpace(returnURL) != "" {
		query := parsed.Query()
		query.Set("redirect_url", returnURL) // 回跳由URL包编码，不能直接拼接用户可见参数。
		parsed.RawQuery = query.Encode()
		h5URL = parsed.String()
	}
	return &OfficialPaymentResult{RedirectURL: h5URL}, nil
}

// 微信关单使用订单号路径和原商户号，禁止跨商户关闭订单。
// 调用者须先从数据库核对订单所属用户和渠道。
// 商户客户端与下单共用公钥验签设置，不允许忽略响应签名。
// 订单号作为路径段编码，避免特殊字符改变上游API路径。
// 仅在微信返回冲突时进行一次短重试，避免并发关单造成误报。
// 其他错误立即返回，不添加隐藏的无限重试循环。
// 每次响应都释放正文，后续重试不会占用前一次连接。
// 成功HTTP状态表示关单成功，随后才允许本地替换订单。
// 重试等待响应调用取消，用户离开或超时不会继续后台关单。
// 第二次冲突仍失败，外层不得将其视为已支付或已关闭。
// 关单过程中不修改本地支付金额、冻结额度或订单归属。
// 本函数不能取消已经发放的订阅，只处理上游待付款交易。
// 失败保留可定位错误，交给控制器提示用户检查真实支付状态。
func closeOfficialWechatPayment(ctx context.Context, channel operation_setting.OfficialPaymentChannel, tradeNo string) error {
	config := channel.Config // 调用者已经取得独立渠道快照。
	// 网络与通知处理都有上限，避免上游故障长期占用工作线程。
	ctx, cancel := context.WithTimeout(ctx, officialPaymentTimeout)
	defer cancel() // 请求完成后释放上下文计时器。
	// 请求签名与响应验签共用已配置的平台公钥模式。
	client, err := newOfficialWechatClient(ctx, config)
	if err != nil {
		return fmt.Errorf("%w: initialize WeChat Pay client", ErrOfficialPaymentConfig)
	}
	endpoint := "https://api.mch.weixin.qq.com/v3/pay/transactions/out-trade-no/" + url.PathEscape(tradeNo) + "/close"
	// 上游并发关单冲突只重试一次，其余错误立即暴露。
	for attempt := range 2 {
		result, err := client.Post(ctx, endpoint, map[string]any{"mchid": config["mchid"]})
		if err != nil {
			return fmt.Errorf("%w: %v", ErrOfficialPaymentRequest, err)
		}
		if result == nil || result.Response == nil {
			return fmt.Errorf("%w: empty WeChat Pay close response", ErrOfficialPaymentResponse)
		}
		// 关单不消费响应正文，但每次都要释放连接。
		if result.Response.Body != nil {
			_ = result.Response.Body.Close()
		}
		if result.Response.StatusCode >= 200 && result.Response.StatusCode < 300 {
			return nil
		}
		if result.Response.StatusCode != http.StatusConflict || attempt == 1 {
			return fmt.Errorf("%w: WeChat Pay close returned HTTP %d", ErrOfficialPaymentResponse, result.Response.StatusCode)
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done(): // 取消时停止等待，不在后台继续替用户关单。
			timer.Stop()
			return fmt.Errorf("%w: close order timeout", ErrOfficialPaymentRequest)
		case <-timer.C:
		}
	}
	return fmt.Errorf("%w: WeChat Pay close failed", ErrOfficialPaymentResponse)
}

// 生成支付宝请求签名串，字段按字典序排列。
// 空字段不参与签名，sign字段自身不能参与其签名。
// 原始值按支付宝协议拼接，不做URL编码后二次签名。
// 表单发送时再进行URL编码，避免签名内容与上游解码后不一致。
// 同一实现供下单和关单复用，避免两条签名规则分叉。
// 这里只处理渠道服务生成的字段，不执行网络或数据库操作。
// 字段排序使用独立键切片，不改变调用者参数映射。
func buildAlipaySignContent(params map[string]string) string {
	keys := make([]string, 0, len(params)) // 收集键而不改变调用方原始字段。
	for key, value := range params {
		if key != "sign" && strings.TrimSpace(value) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys) // 签名协议要求字典序，与映射遍历顺序无关。
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	return strings.Join(parts, "&")
}

// 生成支付宝通知验签串，规则区别于普通下单请求。
// 通知sign_type属于验签元数据，不参与回调签名串。
// 每个字段使用表单解码后的原始值，不能按展示文本重写。
// 复制为签名映射后复用统一排序，避免依赖HTTP参数顺序。
// 字段缺失保留为空值，由统一签名规则排除。
// 不修改收到的表单，后续业务状态与金额解析仍可使用原值。
// 此函数不读取商户密钥，测试可以独立构造协议样例。
func buildAlipayCallbackSignContent(form url.Values) string {
	params := make(map[string]string, len(form))
	for key := range form {
		if strings.EqualFold(key, "sign") || strings.EqualFold(key, "sign_type") || form.Get(key) == "" {
			continue
		}
		params[key] = form.Get(key)
	}
	return buildAlipaySignContent(params)
}

// 用商户RSA私钥签署支付宝请求正文。
// 先统一解析PEM或裸Base64私钥，再根据已验证算法选择摘要。
// RSA2明确使用SHA256，RSA保留既有商户配置兼容能力。
// 摘要是原始签名串的字节，不能在签名前改变换行或编码。
// 签名结果用标准Base64输出，供表单或查询参数传输。
// 解析或签名错误直接返回，不生成空签名占位。
// 此处不记录私钥、签名原文或完整支付URL。
// 签名只证明商户发起请求，不代表支付已经成功。
func signOfficialAlipayContent(content string, rawPrivateKey string, signType string) (string, error) {
	privateKey, err := parseOfficialPaymentPrivateKey(rawPrivateKey)
	if err != nil {
		return "", err
	}
	var digest []byte
	var hash crypto.Hash
	if strings.EqualFold(signType, "RSA") {
		sum := sha1.Sum([]byte(content))
		digest = sum[:]
		hash = crypto.SHA1
	} else {
		sum := sha256.Sum256([]byte(content))
		digest = sum[:]
		hash = crypto.SHA256
	}
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, hash, digest)
	if err != nil {
		return "", fmt.Errorf("%w: sign request", ErrOfficialPaymentConfig)
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

// 解析商户RSA私钥，支持PKCS8和既有PKCS1编码。
// 管理员可能粘贴转义换行，因此先还原显式换行并裁去首尾空白。
// 裸Base64密钥补标准PEM外壳，保留原渠道配置输入习惯。
// PEM无法解码立即失败，不尝试把坏数据用作密码或默认密钥。
// 先解析PKCS8，再尝试PKCS1，顺序与两类常见商户导出一致。
// PKCS8解析成功但类型不是RSA也必须拒绝。
// 错误信息描述失败阶段，不包含私钥本身或原始PEM。
// 返回值仅供请求签名使用，不能进入公开配置接口。
// 此处不生成替代密钥，不允许配置失效后静默换身份。
func parseOfficialPaymentPrivateKey(raw string) (*rsa.PrivateKey, error) {
	// 仅还原粘贴时的转义换行，不修改密钥内部的Base64字符。
	normalized := strings.TrimSpace(strings.ReplaceAll(raw, "\\n", "\n"))
	if normalized == "" {
		return nil, fmt.Errorf("%w: private key is empty", ErrOfficialPaymentConfig)
	}
	if !strings.Contains(normalized, "BEGIN") {
		normalized = "-----BEGIN PRIVATE KEY-----\n" + normalized + "\n-----END PRIVATE KEY-----"
	}
	block, _ := pem.Decode([]byte(normalized)) // PEM解码失败不进入默认密钥路径。
	if block == nil {
		return nil, fmt.Errorf("%w: private key PEM decode failed", ErrOfficialPaymentConfig)
	}
	// PKCS8可能包含其他算法，解析成功后仍需RSA类型判断。
	parsedPKCS8, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		privateKey, ok := parsedPKCS8.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%w: private key is not RSA", ErrOfficialPaymentConfig)
		}
		return privateKey, nil
	}
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse private key", ErrOfficialPaymentConfig)
	}
	return privateKey, nil
}

// 解析支付宝或微信平台RSA公钥，供响应及通知验签。
// 支持PEM与裸Base64配置，但不会下载或信任通知提供的新公钥。
// 转义换行与私钥解析保持一致，避免粘贴格式造成难定位失败。
// 先解析PKIX，再尝试PKCS1，两种格式都必须确认为RSA。
// 缺失或非RSA公钥属于渠道配置错误，不允许跳过验签。
// 验签公钥ID由渠道额外绑定，公钥内容本身不决定通知路由。
// 错误只报告阶段，不把完整公钥或商户配置写入日志。
// 平台公钥虽非私密凭证，配置接口仍按现有字段遮罩规则返回。
// 返回的公钥不会修改原始配置，也不注册全局证书下载任务。
func parseOfficialPaymentPublicKey(raw string) (*rsa.PublicKey, error) {
	// 仅还原粘贴时的转义换行，不修改密钥内部的Base64字符。
	normalized := strings.TrimSpace(strings.ReplaceAll(raw, "\\n", "\n"))
	if normalized == "" {
		return nil, fmt.Errorf("%w: public key is empty", ErrOfficialPaymentConfig)
	}
	if !strings.Contains(normalized, "BEGIN") {
		normalized = "-----BEGIN PUBLIC KEY-----\n" + normalized + "\n-----END PUBLIC KEY-----"
	}
	block, _ := pem.Decode([]byte(normalized)) // PEM解码失败不进入默认密钥路径。
	if block == nil {
		return nil, fmt.Errorf("%w: public key PEM decode failed", ErrOfficialPaymentConfig)
	}
	// PKIX是通用公钥容器，不能只凭解析成功就当RSA使用。
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err == nil {
		publicKey, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%w: public key is not RSA", ErrOfficialPaymentConfig)
		}
		return publicKey, nil
	}
	publicKey, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse public key", ErrOfficialPaymentConfig)
	}
	return publicKey, nil
}

// 归一化付款请求中的客户端IP，供微信支付风控参数使用。
// 接受纯IPv4、IPv6及带端口的地址，端口不会传给上游。
// 合法IP用net包标准形式输出，不自行拆分IPv6冒号。
// 无法解析时返回明确错误，不能用127.0.0.1假装付款人地址。
// 本函数不信任请求体中的payer_client_ip，来源应是服务端ClientIP。
// 有效地址解析不涉及DNS查询，不会访问外部主机。
func normalizeOfficialPaymentIP(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if parsed := net.ParseIP(raw); parsed != nil {
		return parsed.String(), nil
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		if parsed := net.ParseIP(host); parsed != nil {
			return parsed.String(), nil
		}
	}
	return "", fmt.Errorf("%w: invalid payer IP", ErrOfficialPaymentConfig)
}

// 读取支付JSON响应里的有限标量类型。
// 仅用于渠道协议中允许以文本或数值表示的结果字段。
// 未知结构返回空串，由调用方的必填检查报告协议错误。
// 不能用于把任意对象格式化成成功状态或跳转地址。
// 浮点格式使用无额外精度截断的十进制字符串。
// 金额结算不经过此函数，始终使用decimal及订单金额核对。
// 该函数不对缺失字段提供成功默认值。
func paymentString(value any) string {
	// 仅支持响应协议需要的标量，复杂值由调用者视为字段缺失。
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

// newOfficialWechatClient 统一商户请求签名与平台响应验签。
// 配置公钥及其 ID 同时用于请求响应和异步通知，不自动下载证书。
// SDK 会拒绝缺失、过期或不匹配的响应签名，避免只依赖 HTTPS。
// 创建一次支付请求使用的商户客户端，不共享可变渠道状态。
// 私钥负责商户签名，平台公钥负责响应验签，二者用途不可互换。
// 平台公钥ID和公钥一起绑定，避免仅按证书序列号选择未知身份。
// 私钥或公钥解析失败时立即停止，不启用WithoutValidator回退。
// 复用SDK官方公钥模式，同时覆盖请求签名、响应验证与加解密。
// 客户端不启动自动证书下载，部署无需额外证书轮询任务。
// SDK构造错误保持原样向上返回，不能伪装为网络超时。
// 每次调用都使用当前配置，管理员修改公钥后新请求立即生效。
// 上下文用于本次请求范围，不持久化用户会话或支付凭证。
func newOfficialWechatClient(ctx context.Context, config map[string]string) (*core.Client, error) {
	privateKey, err := parseOfficialPaymentPrivateKey(config["merchant_private_key"])
	if err != nil {
		return nil, err
	}
	publicKey, err := parseOfficialPaymentPublicKey(config["wechatpay_public_key"])
	if err != nil {
		return nil, err
	}
	return core.NewClient(ctx, option.WithWechatPayPublicKeyAuthCipher(
		config["mchid"], config["merchant_serial_no"], privateKey,
		config["wechatpay_public_key_id"], publicKey,
	))
}
