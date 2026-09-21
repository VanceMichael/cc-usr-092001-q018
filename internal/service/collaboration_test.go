package service_test

import (
	"testing"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/service"
	"example.com/batch-092001-q018/internal/testsupport"
)

// setupConcludedPair 建立一对已正式缔结的城市关系，返回服务与关系引用。
func setupConcludedPair(t *testing.T) (svc *service.Service, rel string) {
	t.Helper()
	testsupport.ResetSeq()
	svc = service.New()
	registerCity(t, svc, "CITY-0001", "东城")
	registerCity(t, svc, "CITY-0002", "西城")
	propose(t, svc, "REL-0001", "CITY-0001", "CITY-0002")
	concludePair(t, svc, "REL-0001", "CITY-0001", "CITY-0002")
	return svc, "REL-0001"
}

func registerCommitment(t *testing.T, svc *service.Service, ref, rel string, cat domain.CooperationCategory, title, due string) {
	t.Helper()
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeCommitmentRegistered, ref,
		"2026-06-05T00:00:00+08:00", event.RegisterCommitmentPayload{
			Ref: ref, RelationRef: rel, Category: cat,
			Title:      domain.LocalizedText{OriginalLanguage: "zh", Original: title},
			OwnerDepts: map[string]string{"CITY-0001": "DEPT-CULTURE-1"},
			DueAt:      due,
		}))
}

func TestCommitment_DedupAndOverdue(t *testing.T) {
	svc, rel := setupConcludedPair(t)

	registerCommitment(t, svc, "COMM-0001", rel, domain.CatCulture, "联合举办青年艺术节", "2026-10-01T00:00:00+08:00")

	// 同关系、同类别、同标题（仅空白/大小写差异）不得重复登记。
	dup := testsupport.Envelope(t, testsupport.EventID(), event.TypeCommitmentRegistered, "COMM-0002",
		"2026-06-06T00:00:00+08:00", event.RegisterCommitmentPayload{
			Ref: rel, RelationRef: rel, Category: domain.CatCulture,
			Title: domain.LocalizedText{OriginalLanguage: "zh", Original: " 联合举办青年艺术节 "},
			DueAt: "2026-11-01T00:00:00+08:00",
		})
	testsupport.FailIngest(t, svc, dup, domain.CodeDuplicateCommitment)

	// 截止前未完成：不逾期。
	before, err := svc.GetRelationPair(rel, mustTime(t, "2026-09-01T00:00:00+08:00"))
	if err != nil {
		t.Fatal(err)
	}
	if before.Summary.OverdueCommitments != 0 {
		t.Fatalf("截止前不应有逾期，得到 %d", before.Summary.OverdueCommitments)
	}

	// 截止后未完成：逾期，且责任部门随承诺可见。
	after, err := svc.GetRelationPair(rel, mustTime(t, "2026-10-02T00:00:00+08:00"))
	if err != nil {
		t.Fatal(err)
	}
	if after.Summary.OverdueCommitments != 1 {
		t.Fatalf("应有 1 项逾期，得到 %d", after.Summary.OverdueCommitments)
	}
	if after.Commitments[0].OwnerDepts["CITY-0001"] != "DEPT-CULTURE-1" {
		t.Fatal("责任部门未随承诺返回")
	}

	// 完成后不再逾期。
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeCommitmentFulfilled, "COMM-0001",
		"2026-10-03T00:00:00+08:00", event.FulfillCommitmentPayload{Ref: "COMM-0001", At: "2026-10-03T00:00:00+08:00"}))
	done, _ := svc.GetRelationPair(rel, mustTime(t, "2026-10-04T00:00:00+08:00"))
	if done.Summary.OverdueCommitments != 0 || done.Summary.FulfilledCommitments != 1 {
		t.Fatalf("完成后逾期应为0、完成应为1，得到 overdue=%d fulfilled=%d",
			done.Summary.OverdueCommitments, done.Summary.FulfilledCommitments)
	}
}

func TestActivity_CrossTimezoneAndOutcomes(t *testing.T) {
	svc, rel := setupConcludedPair(t)

	// 主办方在 +09:00 时区举办活动，原始偏移与 IANA 时区同时保存。
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeActivityRecorded, "ACT-0001",
		"2026-07-10T19:30:00+09:00", event.RecordActivityPayload{
			Ref: rel, RelationRef: rel, Category: domain.CatTourism,
			Title: domain.LocalizedText{
				OriginalLanguage: "ja", Original: "観光フォーラム",
				Translations: map[string]string{"zh": "旅游论坛", "en": "Tourism Forum"},
			},
			StartAt:     "2026-07-10T19:30:00+09:00",
			EndAt:       "2026-07-10T21:00:00+09:00",
			TimeZone:    "Asia/Tokyo",
			HostPartyRef: "CITY-0002",
			Outcomes: []domain.Outcome{{
				Summary:     domain.LocalizedText{OriginalLanguage: "zh", Original: "签署旅游合作意向"},
				MaterialRef: "DOC-OUTCOME-1",
				Digest:      domain.Digest("sha256:" + repeat64("b")),
			}},
		}))

	view, err := svc.GetRelationPair(rel, mustTime(t, "2026-07-11T00:00:00+09:00"))
	if err != nil {
		t.Fatal(err)
	}
	if view.Summary.TotalActivities != 1 || view.Summary.TotalOutcomes != 1 {
		t.Fatalf("应聚合 1 场活动 1 项成果，得到 %d/%d", view.Summary.TotalActivities, view.Summary.TotalOutcomes)
	}
	act := view.Activities[0]
	if act.StartRaw != "2026-07-10T19:30:00+09:00" || act.TimeZone != "Asia/Tokyo" {
		t.Fatal("跨时区原始偏移与时区必须保留")
	}
	// 多语原文、译文同时保存。
	if act.Title.Original != "観光フォーラム" || act.Title.Translations["zh"] != "旅游论坛" {
		t.Fatal("多语原文/译文未同时保存")
	}
}

func TestPlan_HierarchyLevelAndCityConfirmation(t *testing.T) {
	testsupport.ResetSeq()
	svc := service.New()

	// 国家级路线图。
	registerPlan(t, svc, "PLAN-0001", domain.LevelCountry, func(p *event.RegisterPlanPayload) {
		p.AllowedCategories = []domain.CooperationCategory{domain.CatCulture, domain.CatEducation}
	})
	bindPlan(t, svc, "PLAN-0001", "2026-01-15T00:00:00+08:00")

	// 城市级计划挂在国家路线图之下。
	registerPlan(t, svc, "PLAN-0002", domain.LevelCity, func(p *event.RegisterPlanPayload) {
		p.ParentRef = "PLAN-0001"
	})

	// 层级倒挂（国家计划挂在城市计划下）应拒绝。
	bad := testsupport.Envelope(t, testsupport.EventID(), event.TypePlanRegistered, "PLAN-0003",
		"2026-01-20T00:00:00+08:00", event.RegisterPlanPayload{
			Ref: "PLAN-0003", Level: domain.LevelCountry, ParentRef: "PLAN-0002",
			Title: domain.LocalizedText{OriginalLanguage: "zh", Original: "倒挂计划"},
		})
	testsupport.FailIngest(t, svc, bad, domain.CodeValidation)

	// 城市级计划在城市自身确认后 CityConfirmed 置真。
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypePlanCityConfirmed, "PLAN-0002",
		"2026-01-21T00:00:00+08:00", event.PlanRefPayload{Ref: "PLAN-0002"}))
}

func TestRoadmapCategoryConstraint_BlocksLowerPlanCommitment(t *testing.T) {
	svc, rel := setupConcludedPair(t)

	// 生效国家路线图只允许文化/教育；城市计划受其约束。
	registerPlan(t, svc, "PLAN-1001", domain.LevelCountry, func(p *event.RegisterPlanPayload) {
		p.AllowedCategories = []domain.CooperationCategory{domain.CatCulture, domain.CatEducation}
	})
	bindPlan(t, svc, "PLAN-1001", "2026-01-15T00:00:00+08:00")
	registerPlan(t, svc, "PLAN-1002", domain.LevelCity, func(p *event.RegisterPlanPayload) {
		p.ParentRef = "PLAN-1001"
	})
	bindPlan(t, svc, "PLAN-1002", "2026-01-20T00:00:00+08:00")

	// 旅游事项落在受约束的城市计划下：被生效上级路线图禁止。
	bad := testsupport.Envelope(t, testsupport.EventID(), event.TypeCommitmentRegistered, "COMM-0099",
		"2026-06-06T00:00:00+08:00", event.RegisterCommitmentPayload{
			Ref: "COMM-0099", RelationRef: rel, Category: domain.CatTourism,
			Title:         domain.LocalizedText{OriginalLanguage: "zh", Original: "旅游推广周"},
			SourcePlanRef: "PLAN-1002",
		})
	testsupport.FailIngest(t, svc, bad, domain.CodePlanConstraint)

	// 文化事项允许。
	registerCommitment(t, svc, "COMM-0100", rel, domain.CatCulture, "青年文化交流营", "2026-12-01T00:00:00+08:00")
}
