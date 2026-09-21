package service_test

import (
	"testing"
	"time"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/service"
	"example.com/batch-092001-q018/internal/testsupport"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	tt, err := domain.ParseOccurredAt(value)
	if err != nil {
		t.Fatalf("非法时间 %q: %v", value, err)
	}
	return tt
}

// registerPlan 登记一份指定层级的计划（默认 proposed）。
func registerPlan(t *testing.T, svc *service.Service, ref string, level domain.Level, opts ...func(*event.RegisterPlanPayload)) {
	t.Helper()
	pl := event.RegisterPlanPayload{
		Ref: ref, Level: level,
		Title: domain.LocalizedText{OriginalLanguage: "zh", Original: "计划-" + ref},
	}
	for _, o := range opts {
		o(&pl)
	}
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypePlanRegistered, ref,
		"2026-01-10T00:00:00+08:00", pl))
}

// bindPlan 使一份计划生效为 binding。
func bindPlan(t *testing.T, svc *service.Service, ref, effective string) {
	t.Helper()
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypePlanBound, ref,
		effective, event.BindPlanPayload{Ref: ref, EffectiveAt: effective}))
}
