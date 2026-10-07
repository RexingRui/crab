package service

import (
	"context"
	"errors"
	"unicode/utf8"

	"crab-order/internal/errs"
	"crab-order/internal/model"
	"crab-order/internal/store"
	"crab-order/internal/timex"
)

// SpecService 管理规格价目表。改价只影响新订单：订单明细里的单价是快照，
// 不与 specs 关联，历史订单金额不会跟着变。
type SpecService struct {
	st  store.Store
	now func() int64
}

func NewSpecService(st store.Store) *SpecService {
	return &SpecService{st: st, now: timex.Now}
}

func (s *SpecService) SetClock(f func() int64) { s.now = f }

// SpecInput 一档规格：性别 + 克重 + 品相，一只多少钱。
type SpecInput struct {
	Gender    model.Gender
	SpecGram  int
	Grade     model.Grade // 空串按正常处理
	SpecLabel string
	// UnitPriceMilli 单只价，单位「厘」（0.001 元）。
	UnitPriceMilli int64
	Enabled        *bool
	SortNo         int
}

func (in *SpecInput) validate() error {
	if !in.Gender.Valid() {
		return errs.InvalidParam("gender 非法，应为 male/female")
	}
	if in.Grade == "" {
		in.Grade = model.GradeNormal
	}
	if !in.Grade.Valid() {
		return errs.InvalidParam("grade 非法，应为 normal/broken")
	}
	if in.SpecGram <= 0 {
		return errs.InvalidParam("spec_gram 必须大于 0")
	}
	if n := utf8.RuneCountInString(in.SpecLabel); n < 1 || n > 32 {
		return errs.InvalidParam("spec_label 必填，长度 1-32 字符")
	}
	if in.UnitPriceMilli < 0 {
		return errs.InvalidParam("unit_price_milli 不能为负")
	}
	return nil
}

// List 列出规格。onlyEnabled 为 true 时只返回启用中的（小程序录单页下拉用）。
func (s *SpecService) List(ctx context.Context, onlyEnabled bool) ([]model.Spec, error) {
	list, err := s.st.ListSpecs(ctx, onlyEnabled)
	if err != nil {
		return nil, errs.Internal(err)
	}
	return list, nil
}

func (s *SpecService) Create(ctx context.Context, in SpecInput) (*model.Spec, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	sp := &model.Spec{
		Gender:         in.Gender,
		SpecGram:       in.SpecGram,
		Grade:          in.Grade,
		SpecLabel:      in.SpecLabel,
		UnitPriceMilli: in.UnitPriceMilli,
		Enabled:        enabled,
		SortNo:         in.SortNo,
		UpdatedAt:      s.now(),
	}
	if err := s.st.InsertSpec(ctx, sp); err != nil {
		if store.IsUniqueViolation(err) {
			return nil, errs.InvalidParam("同样的 性别 + 克重 + 品相 已存在")
		}
		return nil, errs.Internal(err)
	}
	return sp, nil
}

func (s *SpecService) Update(ctx context.Context, id int64, in SpecInput) (*model.Spec, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	sp, err := s.st.GetSpecByID(ctx, id)
	if err != nil {
		return nil, mapNotFound(err, "规格")
	}
	sp.Gender = in.Gender
	sp.SpecGram = in.SpecGram
	sp.Grade = in.Grade
	sp.SpecLabel = in.SpecLabel
	sp.UnitPriceMilli = in.UnitPriceMilli
	if in.Enabled != nil {
		sp.Enabled = *in.Enabled
	}
	sp.SortNo = in.SortNo
	sp.UpdatedAt = s.now()

	if err := s.st.UpdateSpec(ctx, sp); err != nil {
		if store.IsUniqueViolation(err) {
			return nil, errs.InvalidParam("同样的 性别 + 克重 + 品相 已存在")
		}
		if errors.Is(err, errs.ErrNotFound) {
			return nil, errs.NotFound("规格")
		}
		return nil, errs.Internal(err)
	}
	return sp, nil
}

// Disable 停用规格（置 enabled = 0），不做物理删除。
func (s *SpecService) Disable(ctx context.Context, id int64) error {
	if err := s.st.DisableSpec(ctx, id, s.now()); err != nil {
		return mapNotFound(err, "规格")
	}
	return nil
}
