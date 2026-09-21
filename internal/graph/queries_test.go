package graph

import (
	"testing"
	"time"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/events"
)

func buildPair(t *testing.T, g *Graph) {
	t.Helper()
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-A", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "阿城"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-B", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "波城"}})
	mustApply(t, g, events.RelationProposed{Ref: "REL-100", EntityA: "ENT-A", EntityB: "ENT-B", ResponsibleDept: "外办"})
}

func TestRelationDossierCommitmentsOverdueAndActivities(t *testing.T) {
	g := New()
	buildPair(t, g)

	// 两项文化承诺，一项未到期、一项已逾期；另有一项教育承诺已兑现。
	mustApply(t, g, events.CommitmentLogged{
		Ref: "CMT-OPEN", RelationRef: "REL-100", Category: domain.CatCulture,
		Description:     domain.LocalizedText{"zh": "明年互办文化周"},
		ResponsibleDept: "文旅局", DueDate: "2026-12-31",
	})
	mustApply(t, g, events.CommitmentLogged{
		Ref: "CMT-LATE", RelationRef: "REL-100", Category: domain.CatCulture,
		Description:     domain.LocalizedText{"zh": "本应年初完成的展览"},
		ResponsibleDept: "文旅局", DueDate: "2026-03-01",
	})
	mustApply(t, g, events.CommitmentLogged{
		Ref: "CMT-DONE", RelationRef: "REL-100", Category: domain.CatEducation,
		Description:     domain.LocalizedText{"zh": "交换生项目"},
		ResponsibleDept: "教育局", DueDate: "2026-05-01",
	})
	// 跨时区活动：北京时间 2026-04-20 20:00（UTC 12:00），关联已兑现承诺。
	mustApply(t, g, events.ActivityHeld{
		Ref: "ACT-001", RelationRef: "REL-100", Category: domain.CatEducation,
		Title:           domain.LocalizedText{"zh": "线上青年交流", "en": "Online Youth Exchange"},
		StartAt:         "2026-04-20T20:00:00+08:00",
		EndAt:           "2026-04-20T22:00:00+08:00",
		Timezone:        "Asia/Shanghai",
		Outcomes:        domain.LocalizedText{"zh": "签署交换生备忘录", "en": "MoU signed"},
		CommitmentRefs:  []string{"CMT-DONE"},
		ResponsibleDept: "青联",
	})
	mustApply(t, g, events.CommitmentFulfilled{Ref: "CMT-DONE", FulfilledAt: "2026-04-20", ActivityRef: "ACT-001"})

	// 查询时刻：UTC 2026-09-21 00:00（北京已是上午，绝对时刻比较不受展示时区影响）。
	now := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	d, ok := g.DossierRelation("REL-100", now)
	if !ok {
		t.Fatal("卷宗不存在")
	}
	if len(d.OverdueCommitments) != 1 || d.OverdueCommitments[0].Ref != "CMT-LATE" {
		t.Fatalf("应有一项逾期承诺 CMT-LATE，实得 %+v", d.OverdueCommitments)
	}
	if len(d.OpenCommitments) != 1 || d.OpenCommitments[0].Ref != "CMT-OPEN" {
		t.Fatalf("应有一项未到期承诺 CMT-OPEN，实得 %+v", d.OpenCommitments)
	}
	if len(d.FulfilledCommitments) != 1 || d.FulfilledCommitments[0].FulfillmentActRef != "ACT-001" {
		t.Fatal("已兑现承诺应回指活动成果")
	}
	if len(d.Activities) != 1 || d.Activities[0].Timezone != "Asia/Shanghai" {
		t.Fatal("活动成果与时区应原样保留")
	}
	wantDepts := map[string]bool{"外办": true, "文旅局": true, "教育局": true, "青联": true}
	if len(d.ResponsibleDepts) != len(wantDepts) {
		t.Fatalf("责任部门汇总不符: %v", d.ResponsibleDepts)
	}
	for _, dept := range d.ResponsibleDepts {
		if !wantDepts[dept] {
			t.Fatalf("意外的责任部门 %s", dept)
		}
	}
}

func TestAnalyzeProposalDetectsDuplicateAndResources(t *testing.T) {
	g := New()
	// A-B 之间已有正式缔结关系。
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-A", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "阿城"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-B", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "波城"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-C", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "策城"}})
	mustApply(t, g, events.RelationProposed{Ref: "REL-AB", EntityA: "ENT-A", EntityB: "ENT-B"})

	// A 与 C 的既有网络里有青年活动与开放的旅游承诺，提议 B-C 合作时可复用。
	mustApply(t, g, events.RelationProposed{Ref: "REL-AC", EntityA: "ENT-A", EntityB: "ENT-C"})
	mustApply(t, g, events.CommitmentLogged{
		Ref: "CMT-AC1", RelationRef: "REL-AC", Category: domain.CatTourism,
		Description: domain.LocalizedText{"zh": "联合推介线路"}, ResponsibleDept: "文旅局", DueDate: "2027-01-01",
	})
	mustApply(t, g, events.ActivityHeld{
		Ref: "ACT-AC1", RelationRef: "REL-AC", Category: domain.CatYouth,
		Title: domain.LocalizedText{"zh": "青年圆桌"}, StartAt: "2026-05-01T10:00:00+09:00", Timezone: "Asia/Tokyo",
	})

	// 重复提议：A-B 再合作。
	res, err := g.AnalyzeProposal("ENT-A", "ENT-B")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExistingRelation != "REL-AB" {
		t.Fatalf("应识别既有关系 REL-AB，实得 %q", res.ExistingRelation)
	}
	foundConflict := false
	for _, issue := range res.Issues {
		if issue.Kind == "duplicate_relation" && issue.Severity == "conflict" {
			foundConflict = true
		}
	}
	if !foundConflict {
		t.Fatalf("应报告重复关系冲突: %+v", res.Issues)
	}

	// 新提议 B-C：无冲突，应发现 A-C 网络中可复用的旅游承诺与青年活动。
	res, err = g.AnalyzeProposal("ENT-B", "ENT-C")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExistingRelation != "" {
		t.Fatal("B-C 之间不应有既有关系")
	}
	resourceRefs := map[string]bool{}
	cats := map[string]bool{}
	for _, rr := range res.ReusableResources {
		resourceRefs[rr.Ref] = true
		cats[rr.Category] = true
	}
	if !resourceRefs["CMT-AC1"] || !resourceRefs["ACT-AC1"] {
		t.Fatalf("应复用 CMT-AC1 与 ACT-AC1，实得 %+v", res.ReusableResources)
	}
	if !cats[domain.CatTourism] || !cats[domain.CatYouth] {
		t.Fatalf("应覆盖旅游与青年类别，实得 %v", res.CategoriesCovered)
	}
}

func TestAnalyzeProposalNameCollision(t *testing.T) {
	g := New()
	// 两座同名城市分属不同上级。
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-CN01", Level: domain.LevelCountry, Names: domain.LocalizedText{"zh": "甲国"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-CN02", Level: domain.LevelCountry, Names: domain.LocalizedText{"zh": "乙国"}})
	mustApply(t, g, events.EntityRegistered{
		Ref: "ENT-S1", Level: domain.LevelCity, ParentRef: "ENT-CN01", Names: domain.LocalizedText{"zh": "斯普林菲尔德"},
	})
	mustApply(t, g, events.EntityRegistered{
		Ref: "ENT-S2", Level: domain.LevelCity, ParentRef: "ENT-CN02", Names: domain.LocalizedText{"zh": "斯普林菲尔德"},
	})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-T", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "塔城"}})

	res, err := g.AnalyzeProposal("ENT-S1", "ENT-T")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range res.Issues {
		if issue.Kind == "same_name_side" && issue.EntityRef == "ENT-S2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应提示同名的另一城市 ENT-S2，实得 %+v", res.Issues)
	}
}

func TestSearchEntitiesIncludesHistoricalNames(t *testing.T) {
	g := New()
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-A", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "新市"}})
	mustApply(t, g, events.NameAttached{
		NameRef: "NAM-1", EntityRef: "ENT-A", Name: domain.LocalizedText{"zh": "旧市"},
		ValidFrom: "1980-01-01", Historical: true,
	})
	out, err := g.SearchEntities("zh", "旧市")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Ref != "ENT-A" {
		t.Fatalf("按历史名称应能查到实体，实得 %+v", out)
	}
}
