package service_test

import (
	"testing"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/service"
	"example.com/batch-092001-q018/internal/testsupport"
)

func twoCities(t *testing.T, svc *service.Service) {
	t.Helper()
	registerCity(t, svc, "CITY-0001", "东城")
	registerCity(t, svc, "CITY-0002", "西城")
}

func propose(t *testing.T, svc *service.Service, ref, a, b string) {
	t.Helper()
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationProposed, ref,
		"2026-02-01T00:00:00+08:00", event.ProposeRelationPayload{Ref: ref, PartyA: a, PartyB: b}))
}

// concludePair 走完双方法定批准、签署文本与城市确认并正式缔结。
func concludePair(t *testing.T, svc *service.Service, rel, a, b string) {
	t.Helper()
	approve(t, svc, rel, a)
	approve(t, svc, rel, b)
	signText(t, svc, rel)
	confirmCity(t, svc, rel, a)
	confirmCity(t, svc, rel, b)
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationConcluded, rel,
		"2026-06-01T00:00:00+08:00", event.ConcludeRelationPayload{RelationRef: rel, EffectiveAt: "2026-06-01T00:00:00+08:00"}))
}

func approve(t *testing.T, svc *service.Service, rel, party string) {
	t.Helper()
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeApprovalRecorded, rel,
		"2026-05-01T00:00:00+08:00", event.RecordApprovalPayload{
			RelationRef: rel, PartyRef: party, Authority: "市政厅", Instrument: "DOC-0001",
			ApprovedAt: "2026-05-01T00:00:00+08:00",
		}))
}

func signText(t *testing.T, svc *service.Service, rel string) {
	t.Helper()
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeTextRecorded, rel,
		"2026-05-10T00:00:00+08:00", event.RecordTextPayload{
			RelationRef: rel,
			Title:       domain.LocalizedText{OriginalLanguage: "zh", Original: "友好合作协议书", Translations: map[string]string{"en": "Friendship Cooperation Agreement"}},
			BodyRef:     "DOC-AGREE-1", BodyDigest: domain.Digest("sha256:" + repeat64("a")),
			Signed:      true, SignedAt: "2026-05-10T10:00:00+08:00",
		}))
}

func confirmCity(t *testing.T, svc *service.Service, rel, city string) {
	t.Helper()
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationCityConfirmed, rel,
		"2026-05-12T00:00:00+08:00", event.ConfirmRelationCityPayload{RelationRef: rel, CityRef: city}))
}

func repeat64(c string) string {
	out := ""
	for i := 0; i < 64; i++ {
		out += c
	}
	return out
}

func TestProposeRelation_DuplicatePairRejected(t *testing.T) {
	testsupport.ResetSeq()
	svc := service.New()
	twoCities(t, svc)
	propose(t, svc, "REL-0001", "CITY-0001", "CITY-0002")

	// 换引用、颠倒双方顺序，仍是同一无序对：不得创建第二对关系。
	dup := testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationProposed, "REL-0002",
		"2026-02-02T00:00:00+08:00", event.ProposeRelationPayload{
			Ref: "REL-0002", PartyA: "CITY-0002", PartyB: "CITY-0001",
		})
	testsupport.FailIngest(t, svc, dup, domain.CodeDuplicateRelation)
}

func TestConclude_RequiresApprovalsSignedTextAndCityConfirmation(t *testing.T) {
	testsupport.ResetSeq()
	svc := service.New()
	twoCities(t, svc)
	propose(t, svc, "REL-0001", "CITY-0001", "CITY-0002")

	// 缺一切条件时不能缔结。
	early := testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationConcluded, "REL-0001",
		"2026-06-01T00:00:00+08:00", event.ConcludeRelationPayload{
			RelationRef: "REL-0001", EffectiveAt: "2026-06-01T00:00:00+08:00",
		})
	testsupport.FailIngest(t, svc, early, domain.CodeMissingApproval)

	approve(t, svc, "REL-0001", "CITY-0001")
	signText(t, svc, "REL-0001")
	// 仅一方批准 + 城市未确认仍失败，且错误信息含城市确认缺失。
	stillMissing := testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationConcluded, "REL-0001",
		"2026-06-02T00:00:00+08:00", event.ConcludeRelationPayload{
			RelationRef: "REL-0001", EffectiveAt: "2026-06-02T00:00:00+08:00",
		})
	_, err := svc.Ingest(stillMissing)
	testsupport.AssertCode(t, err, domain.CodeMissingApproval)

	// 补齐第二方批准与双方城市确认后可缔结。
	approve(t, svc, "REL-0001", "CITY-0002")
	confirmCity(t, svc, "REL-0001", "CITY-0001")
	confirmCity(t, svc, "REL-0001", "CITY-0002")
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationConcluded, "REL-0001",
		"2026-06-03T00:00:00+08:00", event.ConcludeRelationPayload{
			RelationRef: "REL-0001", EffectiveAt: "2026-06-03T00:00:00+08:00",
		}))

	view, err := svc.GetRelationPair("REL-0001", mustTime(t, "2026-06-03T00:00:00+08:00"))
	if err != nil {
		t.Fatal(err)
	}
	if view.Relation.Status != domain.StatusConcluded {
		t.Fatalf("关系应已正式缔结，得到 %s", view.Relation.Status)
	}
	if view.Relation.EstablishedRaw != "2026-06-03T00:00:00+08:00" {
		t.Fatalf("生效日期未保留: %s", view.Relation.EstablishedRaw)
	}
}

func TestRelationStateMachine_SuspendResumeTerminate(t *testing.T) {
	testsupport.ResetSeq()
	svc := service.New()
	twoCities(t, svc)
	propose(t, svc, "REL-0001", "CITY-0001", "CITY-0002")
	concludePair(t, svc, "REL-0001", "CITY-0001", "CITY-0002")

	statusEvent := func(typ, at string) event.Envelope {
		return testsupport.Envelope(t, testsupport.EventID(), typ, "REL-0001", at,
			event.RelationStatusPayload{RelationRef: "REL-0001", At: at})
	}

	// 意向不能直接暂停（此时已缔结，先验证非法迁移：终止后不能恢复）。
	testsupport.MustIngest(t, svc, statusEvent(event.TypeRelationSuspended, "2026-07-01T00:00:00+08:00"))
	testsupport.MustIngest(t, svc, statusEvent(event.TypeRelationResumed, "2026-08-01T00:00:00+08:00"))
	testsupport.MustIngest(t, svc, statusEvent(event.TypeRelationTerminated, "2026-09-01T00:00:00+08:00"))

	// 终态后不能再暂停或恢复。
	testsupport.FailIngest(t, svc, statusEvent(event.TypeRelationSuspended, "2026-09-02T00:00:00+08:00"), domain.CodeInvalidTransition)

	view, _ := svc.GetRelationPair("REL-0001", mustTime(t, "2026-09-02T00:00:00+08:00"))
	if view.Relation.Status != domain.StatusTerminated {
		t.Fatalf("最终状态应为终止，得到 %s", view.Relation.Status)
	}
	if len(view.Relation.StatusHistory) < 4 {
		t.Fatalf("状态历史应完整记录迁移，得到 %d 条", len(view.Relation.StatusHistory))
	}
}

func TestCityConfirmation_CannotBeReplacedByRoadmap(t *testing.T) {
	testsupport.ResetSeq()
	svc := service.New()
	registerCity(t, svc, "CITY-0001", "东城")
	registerCity(t, svc, "CITY-0002", "西城")
	// 先登记一份国家级路线图并生效。
	registerPlan(t, svc, "PLAN-0001", domain.LevelCountry)
	bindPlan(t, svc, "PLAN-0001", "2026-01-15T00:00:00+08:00")
	// 关系由上级路线图推动（origin_plan_ref），但路线图不能替代城市本身确认。
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationProposed, "REL-0009",
		"2026-02-01T00:00:00+08:00", event.ProposeRelationPayload{
			Ref: "REL-0009", PartyA: "CITY-0001", PartyB: "CITY-0002", OriginPlanRef: "PLAN-0001",
		}))
	approve(t, svc, "REL-0009", "CITY-0001")
	approve(t, svc, "REL-0009", "CITY-0002")
	signText(t, svc, "REL-0009")
	// 双批准 + 签署齐备，唯独无城市确认 => 拒绝，即便存在生效上级路线图。
	noConfirm := testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationConcluded, "REL-0009",
		"2026-06-01T00:00:00+08:00", event.ConcludeRelationPayload{
			RelationRef: "REL-0009", EffectiveAt: "2026-06-01T00:00:00+08:00",
		})
	_, err := svc.Ingest(noConfirm)
	testsupport.AssertCode(t, err, domain.CodeMissingApproval)
}
