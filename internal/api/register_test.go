package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"crab-order/internal/auth"
	"crab-order/internal/errs"
	"crab-order/internal/model"
	"crab-order/internal/service"
)

// regLink 让卖家签一条登记链接，返回 token。
func (e *testEnv) regLink(t *testing.T, remark string) string {
	t.Helper()
	data := e.mustOK(t, http.MethodPost, "/api/reg-links", map[string]any{"remark": remark})
	var dto regLinkDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		t.Fatalf("解析登记链接失败: %v", err)
	}
	if dto.Token == "" || dto.Path == "" {
		t.Fatalf("登记链接返回空: %+v", dto)
	}
	return dto.Token
}

// publicSpecIDs 取公开价目表里前两档的 id 与单价。
func (e *testEnv) publicSpecIDs(t *testing.T) []PublicSpecDTO {
	t.Helper()
	status, resp, data := e.callWithToken(t, http.MethodGet, "/api/public/specs", nil, "")
	if status != http.StatusOK || resp.Code != errs.CodeOK {
		t.Fatalf("公开价目表失败: status=%d code=%d", status, resp.Code)
	}
	var out struct {
		List []PublicSpecDTO `json:"list"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("解析价目表失败: %v", err)
	}
	if len(out.List) < 2 {
		t.Fatalf("种子规格不足: %d", len(out.List))
	}
	return out.List
}

// registerBody 的 quantity 是只数。
func registerBody(token string, specID int64, quantity int, phone string) map[string]any {
	return map[string]any{
		"token":         token,
		"receiver_name": "李四",
		"phone":         phone,
		"address":       "江苏省苏州市姑苏区平江路 100 号 2 单元 501",
		"wechat_nick":   "四哥",
		"items":         []map[string]any{{"spec_id": specID, "quantity": quantity}},
		"remark":        "麻烦周末发",
	}
}

func (e *testEnv) register(t *testing.T, body map[string]any) (int, Response, json.RawMessage) {
	t.Helper()
	return e.callWithToken(t, http.MethodPost, "/api/public/registrations", body, "")
}

// TestRegistrationFlow 买家自助登记全链路：签链接 → 拉价目表 → 提交 → 落成 source=web 的单。
func TestRegistrationFlow(t *testing.T) {
	e := newTestEnv(t)

	token := e.regLink(t, "老张介绍")
	specs := e.publicSpecIDs(t)
	sp := specs[0]

	status, resp, data := e.register(t, registerBody(token, sp.ID, 16, "13900139001"))
	if status != http.StatusOK || resp.Code != errs.CodeOK {
		t.Fatalf("登记失败: status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}
	var reg RegistrationDTO
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}
	// 金额由服务端按 specs 当前价算，回执里的应收要对得上：16 只正好是两个「8 只价」。
	if want := 2 * sp.PackHintAmount; reg.PayableAmount != want {
		t.Fatalf("应收算错: got=%d want=%d", reg.PayableAmount, want)
	}
	if reg.Idempotent {
		t.Fatal("首次提交不该是幂等命中")
	}

	// 卖家侧能看到这笔单，来源是 web，备注名是签链接时预填的那个。
	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	if o.Source != "web" {
		t.Fatalf("来源错: %s", o.Source)
	}
	if o.WechatRemark != "老张介绍" {
		t.Fatalf("备注名没带上: %q", o.WechatRemark)
	}
	if o.ShipStatus != "pending" || o.PayStatus != "unpaid" {
		t.Fatalf("初始状态错: ship=%s pay=%s", o.ShipStatus, o.PayStatus)
	}
	if o.PayableAmount != 2*sp.PackHintAmount || o.CrabCount != 16 {
		t.Fatalf("落库应收或只数错: payable=%d crabs=%d", o.PayableAmount, o.CrabCount)
	}
	if len(o.Items) != 1 || o.Items[0].Quantity != 16 || o.Items[0].Gender != sp.Gender {
		t.Fatalf("一档该落成一行按只记的明细: %+v", o.Items)
	}
}

// TestPublicSpecsPackHint 页面上「8 只 = xx 元」的那个价由后端给，种子档 8 只正好是整盒价。
func TestPublicSpecsPackHint(t *testing.T) {
	e := newTestEnv(t)
	status, resp, data := e.callWithToken(t, http.MethodGet, "/api/public/specs", nil, "")
	if status != http.StatusOK || resp.Code != errs.CodeOK {
		t.Fatalf("公开价目表失败: code=%d", resp.Code)
	}
	var out struct {
		List        []PublicSpecDTO `json:"list"`
		MinQuantity int             `json:"min_quantity"`
		PackHint    int             `json:"pack_hint"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.PackHint != PackHint || out.MinQuantity != service.RegMinCrabs {
		t.Fatalf("pack_hint=%d min_quantity=%d", out.PackHint, out.MinQuantity)
	}
	sp := out.List[0]
	if sp.Title != "母2.5两" || sp.UnitPriceYuan != "23.625" || sp.PackHintAmountYuan != "189.00" {
		t.Fatalf("第一档展示字段不对: %+v", sp)
	}
}

// TestRegistrationIgnoresClientPrice 买家传什么价都不作数，单价只认 specs 表。
// 这是这条公开写路径上唯一不能破的规矩：破了就是买家自己定价。
func TestRegistrationIgnoresClientPrice(t *testing.T) {
	e := newTestEnv(t)

	token := e.regLink(t, "")
	sp := e.publicSpecIDs(t)[0]

	body := registerBody(token, sp.ID, PackHint, "13900139002")
	// 往请求里塞满各种降价字段，全都该被无视。
	body["items"] = []map[string]any{{"spec_id": sp.ID, "quantity": PackHint,
		"unit_price": 1, "unit_price_milli": 1, "amount": 1, "grade": "broken", "gender": "male"}}
	body["freight_fee"] = -10000
	body["discount"] = 999999
	body["goods_amount"] = 1
	body["payable_amount"] = 1

	status, resp, data := e.register(t, body)
	if status != http.StatusOK || resp.Code != errs.CodeOK {
		t.Fatalf("登记失败: status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}
	var reg RegistrationDTO
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}
	if want := sp.PackHintAmount; reg.PayableAmount != want {
		t.Fatalf("买家改价生效了: got=%d want=%d", reg.PayableAmount, want)
	}

	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	if o.FreightFee != 0 || o.Discount != 0 {
		t.Fatalf("运费/优惠被买家写进去了: freight=%d discount=%d", o.FreightFee, o.Discount)
	}
	it := o.Items[0]
	if it.UnitPriceMilli != sp.UnitPriceMilli || it.Grade != sp.Grade || it.Gender != sp.Gender {
		t.Fatalf("明细快照被买家改了: %+v", it)
	}
}

// TestRegistrationSameLinkIsIdempotent 同一条链接重复提交只会有一笔单。
// 这是「一条链接只落一单」的兜底：靠 jti 当 request_id 撞唯一索引，不靠服务端记账。
func TestRegistrationSameLinkIsIdempotent(t *testing.T) {
	e := newTestEnv(t)

	token := e.regLink(t, "")
	sp := e.publicSpecIDs(t)[0]
	body := registerBody(token, sp.ID, PackHint, "13900139003")

	_, _, first := e.register(t, body)
	var one RegistrationDTO
	if err := json.Unmarshal(first, &one); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}

	// 换个收货人和数量再提交一次，仍然应该拿回第一笔单。
	body["receiver_name"] = "王五"
	body["items"] = []map[string]any{{"spec_id": sp.ID, "quantity": 3 * PackHint}}
	status, resp, second := e.register(t, body)
	if status != http.StatusOK || resp.Code != errs.CodeOK {
		t.Fatalf("重复提交不该报错: status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}
	var two RegistrationDTO
	if err := json.Unmarshal(second, &two); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}
	if two.OrderNo != one.OrderNo {
		t.Fatalf("重复提交建了新单: %s vs %s", one.OrderNo, two.OrderNo)
	}
	if !two.Idempotent {
		t.Fatal("重复提交该带 idempotent: true")
	}
	if two.PayableAmount != one.PayableAmount {
		t.Fatal("重复提交改掉了金额")
	}
	// 链接可能被转发，幂等回执里的姓名要打码：拿到转发链接的人不该看到第一位买家的全名。
	if two.ReceiverName == one.ReceiverName || !strings.Contains(two.ReceiverName, "*") {
		t.Fatalf("幂等回执没给姓名打码: %q", two.ReceiverName)
	}
}

// TestRegistrationAfterSellerDeleted 卖家删了这条链接登记的单，再提交要给明确提示，不能 500。
func TestRegistrationAfterSellerDeleted(t *testing.T) {
	e := newTestEnv(t)

	token := e.regLink(t, "")
	sp := e.publicSpecIDs(t)[0]
	body := registerBody(token, sp.ID, PackHint, "13900139020")

	_, _, data := e.register(t, body)
	var reg RegistrationDTO
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}
	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	e.mustOK(t, http.MethodDelete, "/api/orders/"+strconv.FormatInt(o.ID, 10), nil)

	status, resp, _ := e.register(t, body)
	if resp.Code != errs.CodeStateConflict {
		t.Fatalf("删单后再提交应提示已撤销: status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}
	if !strings.Contains(resp.Msg, "撤销") {
		t.Fatalf("提示语不对: %s", resp.Msg)
	}
}

// TestRegistrationLooseCeil 散买不加价，只是零头向上取整到元：5 只 × 23.625 = 118.125 → 119。
func TestRegistrationLooseCeil(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	_, resp, data := e.register(t, registerBody(e.regLink(t, ""), sp.ID, 5, "13900139030"))
	if resp.Code != errs.CodeOK {
		t.Fatalf("5 只该放行: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var reg RegistrationDTO
	_ = json.Unmarshal(data, &reg)
	if reg.PayableAmount != 11900 || reg.PayableAmount != model.LineAmount(5, sp.UnitPriceMilli) {
		t.Fatalf("5 只的金额算错: %d", reg.PayableAmount)
	}

	// 13 只 = 8 只的价 + 5 只的价向上取整，不再拆成整盒 + 散只两行
	_, resp, data = e.register(t, registerBody(e.regLink(t, ""), sp.ID, 13, "13900139033"))
	if resp.Code != errs.CodeOK {
		t.Fatalf("13 只该放行: code=%d msg=%s", resp.Code, resp.Msg)
	}
	_ = json.Unmarshal(data, &reg)
	if want := model.LineAmount(13, sp.UnitPriceMilli); reg.PayableAmount != want {
		t.Fatalf("13 只的金额算错: got=%d want=%d", reg.PayableAmount, want)
	}
	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	if len(o.Items) != 1 || o.Items[0].Quantity != 13 {
		t.Fatalf("一档该是一行: %+v", o.Items)
	}
}

// TestRegistrationMixesSpecs 自由搭配：一次登记混几档不同的规格。
func TestRegistrationMixesSpecs(t *testing.T) {
	e := newTestEnv(t)
	specs := e.publicSpecIDs(t)

	body := registerBody(e.regLink(t, ""), specs[0].ID, PackHint, "13900139014")
	body["items"] = []map[string]any{
		{"spec_id": specs[0].ID, "quantity": 16},
		{"spec_id": specs[2].ID, "quantity": 8},
	}
	_, resp, data := e.register(t, body)
	if resp.Code != errs.CodeOK {
		t.Fatalf("混档登记失败: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var reg RegistrationDTO
	_ = json.Unmarshal(data, &reg)
	if want := 2*specs[0].PackHintAmount + specs[2].PackHintAmount; reg.PayableAmount != want {
		t.Fatalf("混档金额算错: got=%d want=%d", reg.PayableAmount, want)
	}

	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	if len(o.Items) != 2 {
		t.Fatalf("该有两条明细，实际 %d", len(o.Items))
	}
}

// TestRegistrationMinIsWholeOrder 起订量卡整单：跨档凑够 5 只要放行，一共不够才拒。
func TestRegistrationMinIsWholeOrder(t *testing.T) {
	e := newTestEnv(t)
	specs := e.publicSpecIDs(t)

	// 第一档 1 只 + 第二档 4 只 = 5 只
	body := registerBody(e.regLink(t, ""), specs[0].ID, 1, "13900139031")
	body["items"] = []map[string]any{
		{"spec_id": specs[0].ID, "quantity": 1},
		{"spec_id": specs[1].ID, "quantity": 4},
	}
	_, resp, data := e.register(t, body)
	if resp.Code != errs.CodeOK {
		t.Fatalf("跨档凑够 5 只该放行: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var reg RegistrationDTO
	_ = json.Unmarshal(data, &reg)
	want := model.LineAmount(1, specs[0].UnitPriceMilli) + model.LineAmount(4, specs[1].UnitPriceMilli)
	if reg.PayableAmount != want {
		t.Fatalf("金额算错: got=%d want=%d", reg.PayableAmount, want)
	}

	// 一共 4 只 → 拒，提示里要说清还差多少
	body = registerBody(e.regLink(t, ""), specs[0].ID, 1, "13900139032")
	body["items"] = []map[string]any{
		{"spec_id": specs[0].ID, "quantity": 1},
		{"spec_id": specs[1].ID, "quantity": 3},
	}
	status, resp, _ := e.register(t, body)
	if resp.Code != errs.CodeInvalidParam {
		t.Fatalf("一共 4 只该被拒: status=%d code=%d", status, resp.Code)
	}
	if !strings.Contains(resp.Msg, "4") {
		t.Fatalf("提示里该说清现在有几只: %s", resp.Msg)
	}
}

// TestRegistrationMinQuantity 起订只数：一共不够 5 只直接拒，够了就放行。
func TestRegistrationMinQuantity(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	body := registerBody(e.regLink(t, ""), sp.ID, service.RegMinCrabs-1, "13900139020")
	status, resp, _ := e.register(t, body)
	if resp.Code != errs.CodeInvalidParam {
		t.Fatalf("4 只该被拒: status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}

	body = registerBody(e.regLink(t, ""), sp.ID, service.RegMinCrabs, "13900139021")
	if _, resp, _ = e.register(t, body); resp.Code != errs.CodeOK {
		t.Fatalf("%d 只该放行: code=%d msg=%s", service.RegMinCrabs, resp.Code, resp.Msg)
	}
}

// TestRegistrationDuplicatePhone 换一条链接、同一个手机号，一天内拦下来。
func TestRegistrationDuplicatePhone(t *testing.T) {
	e := newTestEnv(t)

	sp := e.publicSpecIDs(t)[0]
	phone := "13900139004"

	if status, resp, _ := e.register(t, registerBody(e.regLink(t, ""), sp.ID, PackHint, phone)); resp.Code != errs.CodeOK {
		t.Fatalf("首次登记失败: status=%d code=%d", status, resp.Code)
	}

	status, resp, _ := e.register(t, registerBody(e.regLink(t, ""), sp.ID, PackHint, phone))
	if resp.Code != errs.CodeIdempotent {
		t.Fatalf("重复手机号没被拦: status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}
	// 提示里不能出现单号：链接可能被转发，不该让人拿手机号反查出别人的单号。
	if resp.Msg == "" || containsOrderNo(resp.Msg) {
		t.Fatalf("重复提示泄露了单号: %s", resp.Msg)
	}
}

func containsOrderNo(msg string) bool {
	digits := 0
	for _, r := range msg {
		if r >= '0' && r <= '9' {
			digits++
			if digits >= 8 {
				return true
			}
			continue
		}
		digits = 0
	}
	return false
}

// TestRegistrationBadToken 无 token / 乱改的 token / 登录 token 都进不来。
func TestRegistrationBadToken(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	cases := map[string]string{
		"空 token":   "",
		"乱写":        "not-a-token",
		"签名被改":      e.regLink(t, "") + "x",
		"拿登录 token": e.token,
	}
	for name, tk := range cases {
		t.Run(name, func(t *testing.T) {
			status, resp, _ := e.register(t, registerBody(tk, sp.ID, PackHint, "13900139005"))
			if resp.Code != errs.CodeUnauthorized {
				t.Fatalf("该拒绝却放行了: status=%d code=%d", status, resp.Code)
			}
		})
	}
}

// TestRegistrationExpiredToken 过期的链接进不来。
func TestRegistrationExpiredToken(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	// 直接用同一个密钥签一个负 TTL 的 token，等价于一条已经过期的链接。
	expired, _, err := auth.NewRegSigner(testSecret, -time.Hour).Issue(testAdminID, "", time.Now())
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	status, resp, _ := e.register(t, registerBody(expired, sp.ID, PackHint, "13900139006"))
	if resp.Code != errs.CodeUnauthorized {
		t.Fatalf("过期链接被放行: status=%d code=%d", status, resp.Code)
	}
}

// TestRegistrationRejectsDisabledSpec 停用的规格不能再被登记进来。
func TestRegistrationRejectsDisabledSpec(t *testing.T) {
	e := newTestEnv(t)

	specs := e.publicSpecIDs(t)
	sp := specs[0]
	e.mustOK(t, http.MethodDelete, "/api/specs/"+itoa(sp.ID), nil) // 停用

	// 公开价目表里应该也看不到它了。
	for _, s := range e.publicSpecIDs(t) {
		if s.ID == sp.ID {
			t.Fatal("停用的规格还挂在公开价目表上")
		}
	}

	status, resp, _ := e.register(t, registerBody(e.regLink(t, ""), sp.ID, PackHint, "13900139007"))
	if resp.Code != errs.CodeInvalidParam {
		t.Fatalf("停用规格被收下了: status=%d code=%d", status, resp.Code)
	}
}

// TestRegLinkRequiresLogin 签链接是卖家的动作，必须登录。
func TestRegLinkRequiresLogin(t *testing.T) {
	e := newTestEnv(t)
	status, resp, _ := e.callWithToken(t, http.MethodPost, "/api/reg-links", map[string]any{}, "")
	if resp.Code != errs.CodeUnauthorized {
		t.Fatalf("没登录也能签链接: status=%d code=%d", status, resp.Code)
	}
}

// TestDashboardCountsCrabs 看板只数就是明细只数之和；同一档（性别 + 克重 + 品相）合成一行。
func TestDashboardCountsCrabs(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	if status, resp, _ := e.register(t, registerBody(e.regLink(t, ""), sp.ID, 13, "13900139031")); resp.Code != errs.CodeOK {
		t.Fatalf("登记失败: status=%d msg=%s", status, resp.Msg)
	}
	if status, resp, _ := e.register(t, registerBody(e.regLink(t, ""), sp.ID, 8, "13900139032")); resp.Code != errs.CodeOK {
		t.Fatalf("登记失败: status=%d msg=%s", status, resp.Msg)
	}
	// 卖家录同一档，临时改了价——改价不影响分组
	manual := sampleOrderBody()
	manual["items"] = []map[string]any{{
		"gender": sp.Gender, "spec_gram": sp.SpecGram, "grade": sp.Grade, "spec_label": sp.SpecLabel,
		"quantity": 8, "unit_price_milli": 30000,
	}}
	o := decodeOrder(t, e.mustOK(t, http.MethodPost, "/api/orders", manual))
	if o.CrabCount != 8 || o.GoodsAmount != 24000 {
		t.Fatalf("录单只数或货款不对: crabs=%d goods=%d", o.CrabCount, o.GoodsAmount)
	}

	var d struct {
		Range struct {
			CrabCount int `json:"crab_count"`
			BySpec    []struct {
				SpecLabel string `json:"spec_label"`
				Grade     string `json:"grade"`
				Quantity  int    `json:"quantity"`
			} `json:"by_spec"`
		} `json:"range"`
	}
	if err := json.Unmarshal(e.mustOK(t, http.MethodGet, "/api/stats/dashboard", nil), &d); err != nil {
		t.Fatalf("解析看板失败: %v", err)
	}
	if want := 13 + 8 + 8; d.Range.CrabCount != want {
		t.Fatalf("看板只数 %d，期望 %d", d.Range.CrabCount, want)
	}
	if len(d.Range.BySpec) != 1 || d.Range.BySpec[0].Quantity != 29 || d.Range.BySpec[0].Grade != "normal" {
		t.Fatalf("同一档该合成一行: %+v", d.Range.BySpec)
	}
}
