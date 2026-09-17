package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"crab-order/internal/auth"
	"crab-order/internal/errs"
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

// registerBody 的 quantity 是**只数**，不是盒数：8 只装的档填 8 就是一盒。
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

	status, resp, data := e.register(t, registerBody(token, sp.ID, 2*sp.PackSize, "13900139001"))
	if status != http.StatusOK || resp.Code != errs.CodeOK {
		t.Fatalf("登记失败: status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}
	var reg RegistrationDTO
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}
	// 金额由服务端按 specs 当前价算，回执里的应收要对得上。
	if want := sp.UnitPrice * 2; reg.PayableAmount != want {
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
	if o.PayableAmount != sp.UnitPrice*2 {
		t.Fatalf("落库应收算错: %d", o.PayableAmount)
	}
}

// TestRegistrationIgnoresClientPrice 买家传什么价都不作数，单价只认 specs 表。
// 这是这条公开写路径上唯一不能破的规矩：破了就是买家自己定价。
func TestRegistrationIgnoresClientPrice(t *testing.T) {
	e := newTestEnv(t)

	token := e.regLink(t, "")
	sp := e.publicSpecIDs(t)[0]

	body := registerBody(token, sp.ID, sp.PackSize, "13900139002")
	// 往请求里塞满各种降价字段，全都该被无视。
	body["items"] = []map[string]any{{"spec_id": sp.ID, "quantity": sp.PackSize, "unit_price": 1, "amount": 1}}
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
	if want := sp.UnitPrice; reg.PayableAmount != want {
		t.Fatalf("买家改价生效了: got=%d want=%d", reg.PayableAmount, want)
	}

	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	if o.FreightFee != 0 || o.Discount != 0 {
		t.Fatalf("运费/优惠被买家写进去了: freight=%d discount=%d", o.FreightFee, o.Discount)
	}
	if o.Items[0].UnitPrice != sp.UnitPrice {
		t.Fatalf("单价快照被买家改了: %d", o.Items[0].UnitPrice)
	}
}

// TestRegistrationSameLinkIsIdempotent 同一条链接重复提交只会有一笔单。
// 这是「一条链接只落一单」的兜底：靠 jti 当 request_id 撞唯一索引，不靠服务端记账。
func TestRegistrationSameLinkIsIdempotent(t *testing.T) {
	e := newTestEnv(t)

	token := e.regLink(t, "")
	sp := e.publicSpecIDs(t)[0]
	body := registerBody(token, sp.ID, sp.PackSize, "13900139003")

	_, _, first := e.register(t, body)
	var one RegistrationDTO
	if err := json.Unmarshal(first, &one); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}

	// 换个收货人和数量再提交一次，仍然应该拿回第一笔单。
	body["receiver_name"] = "王五"
	body["items"] = []map[string]any{{"spec_id": sp.ID, "quantity": 3 * sp.PackSize}}
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

// TestRegistrationPackRatio 套餐的公母比例：买家能调，价格不跟着变，比例写进明细快照。
func TestRegistrationPackRatio(t *testing.T) {
	e := newTestEnv(t)

	sp := e.publicSpecIDs(t)[0]
	if sp.PackSize != 8 {
		t.Fatalf("种子档应是 8 只装，实际 %d", sp.PackSize)
	}

	body := registerBody(e.regLink(t, ""), sp.ID, 2*sp.PackSize, "13900139010")
	body["items"] = []map[string]any{{"spec_id": sp.ID, "quantity": 2 * sp.PackSize, "male_count": 12}}

	status, resp, data := e.register(t, body)
	if status != http.StatusOK || resp.Code != errs.CodeOK {
		t.Fatalf("登记失败: status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}
	var reg RegistrationDTO
	if err := json.Unmarshal(data, &reg); err != nil {
		t.Fatalf("解析回执失败: %v", err)
	}
	// 一盒就是一盒的价，比例怎么调都不影响金额
	if want := sp.UnitPrice * 2; reg.PayableAmount != want {
		t.Fatalf("比例改动影响了金额: got=%d want=%d", reg.PayableAmount, want)
	}

	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	if !strings.Contains(o.Items[0].SpecLabel, "公12母4") {
		t.Fatalf("比例没写进快照: %q", o.Items[0].SpecLabel)
	}
	if o.Items[0].Unit != "box" || o.Items[0].Quantity != 2 {
		t.Fatalf("套餐该按盒记: unit=%s qty=%d", o.Items[0].Unit, o.Items[0].Quantity)
	}
}

// TestRegistrationPackRatioDefaults 不传比例就是一半一半，传越界的直接拒。
func TestRegistrationPackRatioDefaults(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	// 不传 male_count → 默认 4 公 4 母
	body := registerBody(e.regLink(t, ""), sp.ID, sp.PackSize, "13900139011")
	_, resp, data := e.register(t, body)
	if resp.Code != errs.CodeOK {
		t.Fatalf("登记失败: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var reg RegistrationDTO
	_ = json.Unmarshal(data, &reg)
	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	if !strings.Contains(o.Items[0].SpecLabel, "公4母4") {
		t.Fatalf("默认比例不对: %q", o.Items[0].SpecLabel)
	}

	// male_count=0 是合法的（整盒都要母的），要和「没传」区分开
	body = registerBody(e.regLink(t, ""), sp.ID, sp.PackSize, "13900139012")
	body["items"] = []map[string]any{{"spec_id": sp.ID, "quantity": sp.PackSize, "male_count": 0}}
	_, resp, data = e.register(t, body)
	if resp.Code != errs.CodeOK {
		t.Fatalf("male_count=0 该放行: code=%d msg=%s", resp.Code, resp.Msg)
	}
	_ = json.Unmarshal(data, &reg)
	o = decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	if !strings.Contains(o.Items[0].SpecLabel, "公0母8") {
		t.Fatalf("male_count=0 没生效: %q", o.Items[0].SpecLabel)
	}

	// 超过一盒的只数 → 40001
	body = registerBody(e.regLink(t, ""), sp.ID, sp.PackSize, "13900139013")
	body["items"] = []map[string]any{{"spec_id": sp.ID, "quantity": sp.PackSize, "male_count": sp.PackSize + 1}}
	status, resp, _ := e.register(t, body)
	if resp.Code != errs.CodeInvalidParam {
		t.Fatalf("越界的比例该被拒: status=%d code=%d", status, resp.Code)
	}
}

// TestRegistrationMixesPacks 自由搭配：一次登记混几档不同的套餐。
func TestRegistrationMixesPacks(t *testing.T) {
	e := newTestEnv(t)
	specs := e.publicSpecIDs(t)

	body := registerBody(e.regLink(t, ""), specs[0].ID, specs[0].PackSize, "13900139014")
	body["items"] = []map[string]any{
		{"spec_id": specs[0].ID, "quantity": 2 * specs[0].PackSize, "male_count": 8},
		{"spec_id": specs[2].ID, "quantity": specs[2].PackSize, "male_count": 8},
	}
	_, resp, data := e.register(t, body)
	if resp.Code != errs.CodeOK {
		t.Fatalf("混档登记失败: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var reg RegistrationDTO
	_ = json.Unmarshal(data, &reg)
	if want := specs[0].UnitPrice*2 + specs[2].UnitPrice; reg.PayableAmount != want {
		t.Fatalf("混档金额算错: got=%d want=%d", reg.PayableAmount, want)
	}

	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	if len(o.Items) != 2 {
		t.Fatalf("该有两条明细，实际 %d", len(o.Items))
	}
}

// TestRegistrationSplitsLoose 凑不满一盒的零头单独成一条明细，走散买价。
// 这是「向上取整的单只价只在不按整盒买时才出现」那条规矩的落点。
func TestRegistrationSplitsLoose(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	// 一盒零 5 只
	n := sp.PackSize + 5
	body := registerBody(e.regLink(t, ""), sp.ID, n, "13900139030")
	_, resp, data := e.register(t, body)
	if resp.Code != errs.CodeOK {
		t.Fatalf("一盒零 5 只该放行: code=%d msg=%s", resp.Code, resp.Msg)
	}
	var reg RegistrationDTO
	_ = json.Unmarshal(data, &reg)

	if want := sp.UnitPrice + 5*sp.LoosePrice; reg.PayableAmount != want {
		t.Fatalf("整盒 + 散只的金额算错: got=%d want=%d", reg.PayableAmount, want)
	}

	o := decodeOrder(t, e.mustOK(t, http.MethodGet, "/api/orders/by-no/"+reg.OrderNo, nil))
	if len(o.Items) != 2 {
		t.Fatalf("该拆成两条明细，实际 %d 条", len(o.Items))
	}
	box, loose := o.Items[0], o.Items[1]
	if box.Unit != "box" || box.Quantity != 1 || box.UnitPrice != sp.UnitPrice {
		t.Errorf("整盒那条不对: %+v", box)
	}
	if loose.Unit != "piece" || loose.Quantity != 5 || loose.UnitPrice != sp.LoosePrice {
		t.Errorf("散只那条不对: %+v", loose)
	}
	if !strings.Contains(loose.SpecLabel, "散只") {
		t.Errorf("散只那条的快照该标出来: %q", loose.SpecLabel)
	}
}

// TestRegistrationRejectsShortRemainder 零头不够起订量要拒，并把最近的合法只数说给买家。
func TestRegistrationRejectsShortRemainder(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	// 一盒零 1 只
	body := registerBody(e.regLink(t, ""), sp.ID, sp.PackSize+1, "13900139031")
	status, resp, _ := e.register(t, body)
	if resp.Code != errs.CodeInvalidParam {
		t.Fatalf("零头 1 只该被拒: status=%d code=%d", status, resp.Code)
	}
	// 提示里要给出改成几只，不能只说「不行」
	if !strings.Contains(resp.Msg, itoa(int64(sp.PackSize))) ||
		!strings.Contains(resp.Msg, itoa(int64(sp.PackSize+service.RegMinCrabs))) {
		t.Fatalf("提示没给出可选只数: %s", resp.Msg)
	}
}

// TestRegistrationLoosePriceBeatsNothing 散买价比整盒摊下来贵，凑整盒才划算。
// 这条守着「向上取整」的方向：取整取反了会让散买比整盒便宜。
func TestRegistrationLoosePriceBeatsNothing(t *testing.T) {
	e := newTestEnv(t)
	for _, sp := range e.publicSpecIDs(t) {
		if sp.PackSize == 0 {
			continue
		}
		if got := sp.LoosePrice * int64(sp.PackSize); got <= sp.UnitPrice {
			t.Errorf("「%s」散买 %d 只 %d 分 ≤ 整盒 %d 分，整盒反而不划算",
				sp.SpecLabel, sp.PackSize, got, sp.UnitPrice)
		}
	}
}

// TestRegistrationMinQuantity 起订只数：按只卖的档不够 5 只直接拒，够了就放行。
// 套餐一盒 8 只，天然过线，所以这条要靠一个「按只」的档才测得出来。
func TestRegistrationMinQuantity(t *testing.T) {
	e := newTestEnv(t)

	// 价目表里加一档按只卖的，pack_size=0
	created := e.mustOK(t, http.MethodPost, "/api/specs", map[string]any{
		"gender": "male", "spec_gram": 225, "spec_label": "4.5两", "unit": "piece", "unit_price": 8800,
	})
	var piece SpecDTO
	if err := json.Unmarshal(created, &piece); err != nil {
		t.Fatalf("解析规格失败: %v", err)
	}

	// 起订量跟着价目表一起下发，页面不用自己写死一个数字
	status, resp, data := e.callWithToken(t, http.MethodGet, "/api/public/specs", nil, "")
	if status != http.StatusOK || resp.Code != errs.CodeOK {
		t.Fatalf("公开价目表失败: code=%d", resp.Code)
	}
	var meta struct {
		MinLoose int `json:"min_loose"`
	}
	_ = json.Unmarshal(data, &meta)
	if meta.MinLoose != service.RegMinCrabs {
		t.Fatalf("散买起订量没下发: %d", meta.MinLoose)
	}

	// 4 只 → 不够
	body := registerBody(e.regLink(t, ""), piece.ID, 4, "13900139020")
	status, resp, _ = e.register(t, body)
	if resp.Code != errs.CodeInvalidParam {
		t.Fatalf("4 只该被拒: status=%d code=%d msg=%s", status, resp.Code, resp.Msg)
	}

	// 5 只 → 刚好放行
	body = registerBody(e.regLink(t, ""), piece.ID, service.RegMinCrabs, "13900139021")
	if _, resp, _ = e.register(t, body); resp.Code != errs.CodeOK {
		t.Fatalf("%d 只该放行: code=%d msg=%s", service.RegMinCrabs, resp.Code, resp.Msg)
	}
}

// TestRegistrationPackMeetsMinimum 一盒 8 只，只买一盒也过线。
func TestRegistrationPackMeetsMinimum(t *testing.T) {
	e := newTestEnv(t)
	sp := e.publicSpecIDs(t)[0]

	_, resp, _ := e.register(t, registerBody(e.regLink(t, ""), sp.ID, sp.PackSize, "13900139022"))
	if resp.Code != errs.CodeOK {
		t.Fatalf("整一盒该放行: code=%d msg=%s", resp.Code, resp.Msg)
	}
}

// TestRegistrationDuplicatePhone 换一条链接、同一个手机号，一天内拦下来。
func TestRegistrationDuplicatePhone(t *testing.T) {
	e := newTestEnv(t)

	sp := e.publicSpecIDs(t)[0]
	phone := "13900139004"

	if status, resp, _ := e.register(t, registerBody(e.regLink(t, ""), sp.ID, sp.PackSize, phone)); resp.Code != errs.CodeOK {
		t.Fatalf("首次登记失败: status=%d code=%d", status, resp.Code)
	}

	status, resp, _ := e.register(t, registerBody(e.regLink(t, ""), sp.ID, sp.PackSize, phone))
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
			status, resp, _ := e.register(t, registerBody(tk, sp.ID, sp.PackSize, "13900139005"))
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
	status, resp, _ := e.register(t, registerBody(expired, sp.ID, sp.PackSize, "13900139006"))
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

	status, resp, _ := e.register(t, registerBody(e.regLink(t, ""), sp.ID, sp.PackSize, "13900139007"))
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
