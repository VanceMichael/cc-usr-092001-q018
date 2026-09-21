package graph

import (
	"testing"
	"time"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/events"
)

// mustApply 应用事件，失败即终止；测试中 occurredAt 统一带 +08:00。
func mustApply(t *testing.T, g *Graph, p events.Payload) {
	t.Helper()
	if err := g.Apply(p, "2026-09-01T10:00:00+08:00"); err != nil {
		t.Fatalf("应用 %T 失败: %v", p, err)
	}
}

func applyErr(g *Graph, p events.Payload) error {
	return g.Apply(p, "2026-09-01T10:00:00+08:00")
}

func TestRegisterEntitiesAndNames(t *testing.T) {
	g := New()
	mustApply(t, g, events.EntityRegistered{
		Ref: "ENT-CN01", Level: domain.LevelCountry,
		Names: domain.LocalizedText{"zh": "甲国", "en": "Jia"},
	})
	mustApply(t, g, events.EntityRegistered{
		Ref: "ENT-CN02", Level: domain.LevelCountry,
		Names: domain.LocalizedText{"zh": "乙国", "en": "Yi"},
	})
	mustApply(t, g, events.EntityRegistered{
		Ref: "ENT-CT01", Level: domain.LevelCity, ParentRef: "ENT-CN01",
		Names: domain.LocalizedText{"zh": "甲城市"},
	})

	// 重复标识不得登记。
	if err := applyErr(g, events.EntityRegistered{
		Ref: "ENT-CN01", Level: domain.LevelCountry, Names: domain.LocalizedText{"zh": "重复"},
	}); err == nil {
		t.Fatal("重复实体标识应被拒绝")
	}
	// 上级不存在不得登记。
	if err := applyErr(g, events.EntityRegistered{
		Ref: "ENT-CT99", Level: domain.LevelCity, ParentRef: "ENT-NOPE",
		Names: domain.LocalizedText{"zh": "孤儿城"},
	}); err == nil {
		t.Fatal("上级实体不存在时应被拒绝")
	}

	// 追加历史名称。
	mustApply(t, g, events.NameAttached{
		NameRef: "NAM-OLD01", EntityRef: "ENT-CT01",
		Name: domain.LocalizedText{"zh": "甲城旧名"}, ValidFrom: "1990-01-01", Historical: true,
	})
	d, ok := g.DossierEntity("ENT-CT01")
	if !ok || len(d.Entity.Names) != 2 {
		t.Fatalf("实体应有初始名+旧名共 2 个，实得 %d", len(d.Entity.Names))
	}
}

func TestDuplicateRelationPairRejectedRegardlessOfOrder(t *testing.T) {
	g := New()
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-A", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "阿城"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-B", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "波城"}})

	mustApply(t, g, events.RelationProposed{Ref: "REL-0001", EntityA: "ENT-A", EntityB: "ENT-B"})
	// 以相反顺序再次上报同一对 —— 不得创建第二对关系。
	err := applyErr(g, events.RelationProposed{Ref: "REL-0002", EntityA: "ENT-B", EntityB: "ENT-A"})
	if err == nil {
		t.Fatal("同一实体对重复上报必须被拒绝")
	}

	// 旧关系终止后，允许为同一对登记新的意向关系（旧对仍可追溯）。
	mustApply(t, g, events.ApprovalRecorded{
		Ref: "APR-A1", RelationRef: "REL-0001", Side: domain.SideA,
		Decision: domain.ApprovalApproved, Authority: "甲市议会", DecidedAt: "2026-01-01",
	})
	mustApply(t, g, events.ApprovalRecorded{
		Ref: "APR-B1", RelationRef: "REL-0001", Side: domain.SideB,
		Decision: domain.ApprovalApproved, Authority: "乙市议会", DecidedAt: "2026-01-02",
	})
	mustApply(t, g, events.RelationConcluded{Ref: "REL-0001", SigningDate: "2026-02-01", EffectiveDate: "2026-03-01"})
	mustApply(t, g, events.RelationTerminated{Ref: "REL-0001", At: "2030-05-01T00:00:00+00:00", Reason: "期满"})
	mustApply(t, g, events.RelationProposed{Ref: "REL-0003", EntityA: "ENT-A", EntityB: "ENT-B"})
	if got := g.relations["REL-0003"].view.Status; got != domain.StatusIntended {
		t.Fatalf("终止旧关系后新关系应为意向，实得 %s", got)
	}
}

func TestRelationLifecycleRequiresBothApprovals(t *testing.T) {
	g := New()
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-A", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "阿城"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-B", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "波城"}})
	mustApply(t, g, events.RelationProposed{Ref: "REL-0010", EntityA: "ENT-A", EntityB: "ENT-B"})

	conclude := events.RelationConcluded{Ref: "REL-0010", SigningDate: "2026-02-01", EffectiveDate: "2026-03-01"}
	if err := applyErr(g, conclude); err == nil {
		t.Fatal("双方法定批准未齐备时不得正式缔结")
	}
	mustApply(t, g, events.ApprovalRecorded{
		Ref: "APR-0010A", RelationRef: "REL-0010", Side: domain.SideA,
		Decision: domain.ApprovalApproved, Authority: "甲市议会", DecidedAt: "2026-01-01",
	})
	if err := applyErr(g, conclude); err == nil {
		t.Fatal("只有一侧批准仍不得正式缔结")
	}
	mustApply(t, g, events.ApprovalRecorded{
		Ref: "APR-0010B", RelationRef: "REL-0010", Side: domain.SideB,
		Decision: domain.ApprovalApproved, Authority: "乙市议会", DecidedAt: "2026-01-02",
	})
	mustApply(t, g, conclude)

	// 一侧事后否决不改变已缔结事实，但再次满足双边前不得重复缔结；
	// 状态机：只有 concluded 可暂停，只有 suspended 可恢复。
	if err := applyErr(g, events.RelationResumed{Ref: "REL-0010", At: "2026-06-01T00:00:00+00:00"}); err == nil {
		t.Fatal("非暂停状态不得恢复")
	}
	mustApply(t, g, events.RelationSuspended{Ref: "REL-0010", At: "2026-06-01T00:00:00+00:00", Reason: "审议"})
	if err := applyErr(g, events.RelationSuspended{Ref: "REL-0010", At: "2026-06-02T00:00:00+00:00"}); err == nil {
		t.Fatal("已暂停关系不得再次暂停")
	}
	mustApply(t, g, events.RelationResumed{Ref: "REL-0010", At: "2026-09-01T00:00:00+00:00"})
	r := g.relations["REL-0010"]
	if r.view.Status != domain.StatusConcluded || len(r.view.History) != 4 {
		t.Fatalf("恢复后状态与变迁历史不符: status=%s history=%d", r.view.Status, len(r.view.History))
	}
}

func TestMergeKeepsOldRelationsTraceable(t *testing.T) {
	g := New()
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-OLD", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "旧城"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-NEW", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "新市"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-FRN", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "远城"}})
	mustApply(t, g, events.RelationProposed{Ref: "REL-0020", EntityA: "ENT-OLD", EntityB: "ENT-FRN"})

	mustApply(t, g, events.EntityMerged{FromRef: "ENT-OLD", IntoRef: "ENT-NEW", EffectiveDate: "2025-01-01"})

	// 继受解析。
	if got := g.Resolve("ENT-OLD"); got != "ENT-NEW" {
		t.Fatalf("旧实体应解析到新实体，实得 %s", got)
	}
	// 旧关系不移动、不删除：仍以合并前的两个实体为缔约方并可从旧实体卷宗查到。
	r := g.relations["REL-0020"]
	sides := map[string]bool{r.view.EntityA: true, r.view.EntityB: true}
	if !sides["ENT-OLD"] || !sides["ENT-FRN"] {
		t.Fatal("合并不得改写既有关系的缔约方")
	}
	d, _ := g.DossierEntity("ENT-OLD")
	if len(d.Relations) != 1 || d.SuccessorChain[0] != "ENT-NEW" {
		t.Fatal("旧实体卷宗必须保留旧关系并给出继受链")
	}
	dNew, _ := g.DossierEntity("ENT-NEW")
	if len(dNew.Predecessors) != 1 || dNew.Predecessors[0] != "ENT-OLD" {
		t.Fatal("新实体卷宗应列出前身")
	}
	// 与继受后的新实体配对不得重复创建（已有存续关系）。
	err := applyErr(g, events.RelationProposed{Ref: "REL-0021", EntityA: "ENT-NEW", EntityB: "ENT-FRN"})
	if err == nil {
		t.Fatal("合并后与同一对方的存续关系仍构成重复，必须拒绝")
	}
	// 同一实体不能被合并两次。
	if err := applyErr(g, events.EntityMerged{FromRef: "ENT-OLD", IntoRef: "ENT-NEW", EffectiveDate: "2026-01-01"}); err == nil {
		t.Fatal("已合并实体不得再次合并")
	}
}

func TestMultilingualTextsVersionsAndSignedCopy(t *testing.T) {
	g := New()
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-A", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "阿城"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-B", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "波城"}})
	mustApply(t, g, events.RelationProposed{Ref: "REL-0030", EntityA: "ENT-A", EntityB: "ENT-B"})

	mustApply(t, g, events.TextRecorded{
		Ref: "TXT-ZH1", RelationRef: "REL-0030", Kind: domain.TextOriginal,
		Language: "zh", Content: "中文原文", Version: 1, SignedAt: "2026-02-01",
	})
	mustApply(t, g, events.TextRecorded{
		Ref: "TXT-EN1", RelationRef: "REL-0030", Kind: domain.TextTranslation,
		Language: "en", Content: "English translation", Version: 1,
	})
	// 同语言同类型同版本号不得重复。
	if err := applyErr(g, events.TextRecorded{
		Ref: "TXT-DUP", RelationRef: "REL-0030", Kind: domain.TextOriginal,
		Language: "zh", Content: "重复版本", Version: 1,
	}); err == nil {
		t.Fatal("文本版本重复必须拒绝")
	}
	// 新版本（修订）允许。
	mustApply(t, g, events.TextRecorded{
		Ref: "TXT-ZH2", RelationRef: "REL-0030", Kind: domain.TextOriginal,
		Language: "zh", Content: "中文原文修订版", Version: 2,
	})
	r := g.relations["REL-0030"]
	if len(r.view.Texts) != 3 || !r.view.Texts[0].Signed || r.view.Texts[1].Signed {
		t.Fatalf("多语文本版本与签署标记不符: %+v", r.view.Texts)
	}
}

func TestRoadmapConstraintsDoNotReplaceCityConfirmation(t *testing.T) {
	g := New()
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-C1", Level: domain.LevelCountry, Names: domain.LocalizedText{"zh": "甲国"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-C2", Level: domain.LevelCountry, Names: domain.LocalizedText{"zh": "乙国"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-X", Level: domain.LevelCity, ParentRef: "ENT-C1", Names: domain.LocalizedText{"zh": "甲国西城"}})
	mustApply(t, g, events.EntityRegistered{Ref: "ENT-Y", Level: domain.LevelCity, ParentRef: "ENT-C2", Names: domain.LocalizedText{"zh": "乙国东城"}})
	mustApply(t, g, events.RelationProposed{Ref: "REL-0040", EntityA: "ENT-X", EntityB: "ENT-Y"})

	// 国家间路线图与城市间交往计划。
	mustApply(t, g, events.PlanAdopted{
		Ref: "PLN-ROAD", Tier: domain.PlanRoadmap, EntityA: "ENT-C1", EntityB: "ENT-C2",
		Title: domain.LocalizedText{"zh": "两国合作路线图"}, AdoptedAt: "2026-01-01",
	})
	mustApply(t, g, events.PlanAdopted{
		Ref: "PLN-CITY", Tier: domain.PlanPlan, EntityA: "ENT-X", EntityB: "ENT-Y",
		ParentRef: "PLN-ROAD",
		Title:     domain.LocalizedText{"zh": "东西城交往计划"}, AdoptedAt: "2026-02-01",
	})
	// 未挂接前城市不能确认。
	if err := applyErr(g, events.CityPlanConfirmed{
		PlanRef: "PLN-ROAD", RelationRef: "REL-0040", CityRef: "ENT-X", At: "2026-03-01T00:00:00+00:00",
	}); err == nil {
		t.Fatal("路线图未挂接关系时城市确认必须拒绝")
	}
	// 国家路线图按行政隶属覆盖城市关系，可以挂接。
	mustApply(t, g, events.PlanBoundToRelation{PlanRef: "PLN-ROAD", RelationRef: "REL-0040", At: "2026-03-01T00:00:00+00:00"})
	// 挂接本身不等于城市确认：卷宗中两城都应处于待确认。
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	d, _ := g.DossierRelation("REL-0040", now)
	if len(d.Plans) != 1 || len(d.Plans[0].PendingCities) != 2 || len(d.Plans[0].ConfirmedByCities) != 0 {
		t.Fatalf("上级挂接不得代替城市确认: %+v", d.Plans[0])
	}
	// 非缔约城市不能确认。
	if err := applyErr(g, events.CityPlanConfirmed{
		PlanRef: "PLN-ROAD", RelationRef: "REL-0040", CityRef: "ENT-C1", At: "2026-03-02T00:00:00+00:00",
	}); err == nil {
		t.Fatal("非缔约城市的确认必须拒绝")
	}
	mustApply(t, g, events.CityPlanConfirmed{
		PlanRef: "PLN-ROAD", RelationRef: "REL-0040", CityRef: "ENT-X", At: "2026-03-02T00:00:00+00:00",
	})
	d, _ = g.DossierRelation("REL-0040", now)
	if len(d.Plans[0].ConfirmedByCities) != 1 || len(d.Plans[0].PendingCities) != 1 {
		t.Fatalf("单侧城市确认后状态不符: %+v", d.Plans[0])
	}
}
