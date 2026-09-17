package api

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"crab-order/internal/errs"
	"crab-order/internal/service"
	"crab-order/internal/timex"
)

// 买家自助登记这条线一共三个接口：
//
//	POST /api/reg-links           卖家签一条登记链接（要登录）
//	GET  /api/public/specs        买家登记页的规格与价格（免登录，只给启用中的）
//	POST /api/public/registrations 买家提交登记（免登录，凭链接里的 token）

type regLinkReq struct {
	// Remark 卖家先写好的备注名（如「老张」），落到订单的 wechat_remark，买家改不了。
	Remark string `json:"remark"`
}

type regLinkDTO struct {
	Token     string `json:"token"`
	Path      string `json:"path"`
	ExpiresAt string `json:"expires_at"`
}

// CreateRegLink POST /api/reg-links
//
// 签一条登记链接给买家。token 是无状态的，服务端不存也撤不回，只能靠 REG_LINK_TTL 兜底；
// 「一条链接只能落一单」由 jti 当幂等键保证，不是由服务端记账保证。
// 返回 path 而不是完整 URL：买家页的域名在小程序侧配置（和查单页共用一个域名）。
func (a *API) CreateRegLink(w http.ResponseWriter, r *http.Request) {
	var req regLinkReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}
	remark := strings.TrimSpace(req.Remark)
	if utf8.RuneCountInString(remark) > 32 {
		Fail(w, r, errs.InvalidParam("remark 最长 32 字符"))
		return
	}

	token, claims, err := a.regSigner.Issue(OpenIDFrom(r.Context()), remark, time.Now())
	if err != nil {
		Fail(w, r, err)
		return
	}
	OK(w, regLinkDTO{
		Token:     token,
		Path:      "/r?t=" + token,
		ExpiresAt: timex.Format(claims.Exp),
	})
}

// PublicSpecs GET /api/public/specs
//
// 买家登记页的价目表。只返回启用中的，且只给展示必需的字段：
// 不带 sort_no / updated_at 这类内部信息。
//
// 顺带把起订只数一并给出去，让页面不用自己写一份同样的数字。
func (a *API) PublicSpecs(w http.ResponseWriter, r *http.Request) {
	list, err := a.specs.List(r.Context(), true)
	if err != nil {
		Fail(w, r, err)
		return
	}
	out := make([]PublicSpecDTO, 0, len(list))
	for _, s := range list {
		out = append(out, ToPublicSpecDTO(s))
	}
	OK(w, map[string]any{
		"list":  out,
		"total": len(out),
		// 散买的起订只数。整盒买不受这条限制，页面据此校验零头。
		"min_loose": service.RegMinCrabs,
	})
}

type regItemReq struct {
	SpecID int64 `json:"spec_id"`
	// Quantity 这一档要几只。买家不填盒数——拆成几盒加几只散的由服务端算。
	Quantity int `json:"quantity"`
	// MaleCount 这一档里公的只数。指针是为了区分「没传」和「传了 0」：
	// 没传用默认的一半一半，传 0 就是整档都要母的。
	MaleCount *int `json:"male_count"`
}

// publicRegisterReq 买家提交的登记。
// 注意这里**没有** unit_price / freight_fee / discount：价格由服务端查表决定，
// 运费与优惠是卖家的事，买家页一个字都不该往里塞。
type publicRegisterReq struct {
	Token          string       `json:"token"`
	ReceiverName   string       `json:"receiver_name"`
	Phone          string       `json:"phone"`
	Address        string       `json:"address"`
	WechatNick     string       `json:"wechat_nick"`
	Items          []regItemReq `json:"items"`
	ExpectShipDate string       `json:"expect_ship_date"`
	Remark         string       `json:"remark"`
}

// PublicRegister POST /api/public/registrations
//
// 买家自助登记。凭链接里的 token 放行，落成一笔 source=web 的待发货订单。
// 重复提交同一条链接会命中幂等，返回原来那笔单（code 0 + idempotent: true），不重复建单。
func (a *API) PublicRegister(w http.ResponseWriter, r *http.Request) {
	var req publicRegisterReq
	if err := decodeJSON(r, &req); err != nil {
		Fail(w, r, err)
		return
	}

	claims, err := a.regSigner.Verify(strings.TrimSpace(req.Token), time.Now())
	if err != nil {
		Fail(w, r, err)
		return
	}

	items := make([]service.RegistrationItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, service.RegistrationItemInput{
			SpecID:    it.SpecID,
			Quantity:  it.Quantity,
			MaleCount: it.MaleCount,
		})
	}

	o, idem, err := a.orders.CreateRegistration(r.Context(), service.RegistrationInput{
		JTI:            claims.JTI,
		Issuer:         claims.Issuer,
		ReceiverName:   strings.TrimSpace(req.ReceiverName),
		Phone:          strings.TrimSpace(req.Phone),
		Address:        strings.TrimSpace(req.Address),
		WechatNick:     strings.TrimSpace(req.WechatNick),
		WechatRemark:   claims.Remark,
		Items:          items,
		ExpectShipDate: strings.TrimSpace(req.ExpectShipDate),
		Remark:         strings.TrimSpace(req.Remark),
	})
	if err != nil {
		Fail(w, r, err)
		return
	}

	OK(w, ToRegistrationDTO(o, idem))
}
