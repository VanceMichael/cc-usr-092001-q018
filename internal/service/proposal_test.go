package service_test

import (
	"testing"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/service"
	"example.com/batch-092001-q018/internal/testsupport"
)

func hasFinding(fs []service.Finding, code string) bool {
	for _, f := range fs {
		if string(f.Code) == code {
			return true
		}
	}
	return false
}

func TestAnalyze_ExistingConcludedPair_ReuseRecommendation(t *testing.T) {
	svc, _ := setupConcludedPair(t)

	out, err := svc.AnalyzeProposal(service.AnalyzeProposalRequest{
		PartyA: service.PartyInput{Ref: "CITY-0001"},
		PartyB: service.PartyInput{Ref: "CITY-0002"},
		Categories: []domain.CooperationCategory{domain.CatCulture},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Recommendation != "use_existing_relation" {
		t.Fatalf("已有正式关系应建议复用既有关系，得到 %q", out.Recommendation)
	}
	if !hasFinding(out.Overlaps, "EXISTING_PAIR") || !hasFinding(out.Reusable, "ACTIVE_RELATION") {
		t.Fatal("应报告既有关系重叠与可复用：", out)
	}
}

func TestAnalyze_AmbiguousName_Blocks(t *testing.T) {
	testsupport.ResetSeq()
	svc := service.New()
	registerCity(t, svc, "CITY-0001", "同名市")
	registerCity(t, svc, "CITY-0002", "同名市")

	out, err := svc.AnalyzeProposal(service.AnalyzeProposalRequest{
		PartyA:     service.PartyInput{Name: "同名市"},
		PartyB:     service.PartyInput{Ref: "CITY-0002"},
		Categories: []domain.CooperationCategory{domain.CatCulture},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Recommendation != "resolve_ambiguity" || !hasFinding(out.Conflicts, "NAME_AMBIGUOUS") {
		t.Fatalf("同名歧义应阻断并要求消歧，得到 %q", out.Recommendation)
	}
}

func TestAnalyze_CommonPartnerAndNetworkExperience(t *testing.T) {
	svc, _ := setupConcludedPair(t) // 0001 <-> 0002 已缔结
	registerCity(t, svc, "CITY-0003", "南城")

	// 0001 <-> 0003 也建立正式关系并办一场文化活动。
	propose(t, svc, "REL-0003", "CITY-0001", "CITY-0003")
	concludePair(t, svc, "REL-0003", "CITY-0001", "CITY-0003")
	registerCommitment(t, svc, "COMM-0003", "REL-0003", domain.CatCulture, "合唱节", "2026-12-01T00:00:00+08:00")
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeActivityRecorded, "ACT-0003",
		"2026-08-01T10:00:00+08:00", event.RecordActivityPayload{
			Ref: "ACT-0003", RelationRef: "REL-0003", Category: domain.CatCulture,
			Title:   domain.LocalizedText{OriginalLanguage: "zh", Original: "合唱节"},
			StartAt: "2026-08-01T10:00:00+08:00",
		}))

	// 提议 0002 <-> 0003：两者都与 0001 有关系 => 共同伙伴；0003 有同类文化活动经验。
	out, err := svc.AnalyzeProposal(service.AnalyzeProposalRequest{
		PartyA:     service.PartyInput{Ref: "CITY-0002"},
		PartyB:     service.PartyInput{Ref: "CITY-0003"},
		Categories: []domain.CooperationCategory{domain.CatCulture},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(out.Overlaps, "COMMON_PARTNER") {
		t.Fatal("应发现共同伙伴 CITY-0001")
	}
	if !hasFinding(out.Reusable, "NETWORK_EXPERIENCE") {
		t.Fatal("应发现网络中可复用的同类活动经验")
	}
	if out.Recommendation != "create_relation" {
		t.Fatalf("两者间无直接关系，应建议新建，得到 %q", out.Recommendation)
	}
}

func TestAnalyze_TerminatedRelationAndDuplicateCommitment(t *testing.T) {
	svc, rel := setupConcludedPair(t)
	registerCommitment(t, svc, "COMM-0001", rel, domain.CatCulture, "青年艺术节", "2026-10-01T00:00:00+08:00")

	// 终止关系。
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationTerminated, rel,
		"2026-09-01T00:00:00+08:00", event.RelationStatusPayload{
			RelationRef: rel, At: "2026-09-01T00:00:00+08:00",
		}))

	out, err := svc.AnalyzeProposal(service.AnalyzeProposalRequest{
		PartyA:      service.PartyInput{Ref: "CITY-0001"},
		PartyB:      service.PartyInput{Ref: "CITY-0002"},
		Categories:  []domain.CooperationCategory{domain.CatCulture},
		Commitments: []domain.LocalizedText{{OriginalLanguage: "zh", Original: "青年艺术节"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasFinding(out.Conflicts, "RELATION_TERMINATED") {
		t.Fatal("终止关系应作为阻断冲突")
	}
	if out.Recommendation != "blocked" {
		t.Fatalf("终止关系应阻断，得到 %q", out.Recommendation)
	}
}

func TestAnalyze_RoadmapForbidsCategory(t *testing.T) {
	svc, rel := setupConcludedPair(t)
	registerPlan(t, svc, "PLAN-2001", domain.LevelCountry, func(p *event.RegisterPlanPayload) {
		p.AllowedCategories = []domain.CooperationCategory{domain.CatEducation}
	})
	bindPlan(t, svc, "PLAN-2001", "2026-01-15T00:00:00+08:00")

	// 把该关系挂到只允许教育的生效路线图上。
	// 关系的 origin_plan_ref 在提议时设定；此处直接在分析前通过重新缔结不易修改，
	// 故用一份来源计划链验证：在关系上登记旅游承诺会被投影拒绝，这里改用分析器的类别约束。
	// 为构造 origin_plan_ref，另建一对由该路线图推动的关系。
	registerCity(t, svc, "CITY-0010", "北城")
	registerCity(t, svc, "CITY-0011", "南州")
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationProposed, "REL-2001",
		"2026-02-01T00:00:00+08:00", event.ProposeRelationPayload{
			Ref: "REL-2001", PartyA: "CITY-0010", PartyB: "CITY-0011", OriginPlanRef: "PLAN-2001",
		}))

	out, err := svc.AnalyzeProposal(service.AnalyzeProposalRequest{
		PartyA:     service.PartyInput{Ref: "CITY-0010"},
		PartyB:     service.PartyInput{Ref: "CITY-0011"},
		Categories: []domain.CooperationCategory{domain.CatTourism},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 关系尚处意向且城市未确认，类别禁令也应出现。
	if !hasFinding(out.Conflicts, "CATEGORY_FORBIDDEN_BY_ROADMAP") {
		t.Fatalf("生效路线图禁止旅游类别时应阻断；冲突=%+v", out.Conflicts)
	}
	_ = rel
}
