package domain

// Level 是行政实体或合作计划所处的层级。
type Level string

const (
	LevelCountry Level = "country" // 国家
	LevelState   Level = "state"   // 省州
	LevelCity    Level = "city"    // 城市
)

// Valid 报告层级是否受控。
func (l Level) Valid() bool {
	switch l {
	case LevelCountry, LevelState, LevelCity:
		return true
	default:
		return false
	}
}

// RelationStatus 是友城关系的生命周期状态。
type RelationStatus string

const (
	StatusIntended  RelationStatus = "intended"  // 意向关系
	StatusConcluded RelationStatus = "concluded" // 正式缔结（生效）
	StatusSuspended RelationStatus = "suspended" // 暂停
	StatusTerminated RelationStatus = "terminated" // 终止
)

// Valid 报告关系状态是否受控。
func (s RelationStatus) Valid() bool {
	switch s {
	case StatusIntended, StatusConcluded, StatusSuspended, StatusTerminated:
		return true
	default:
		return false
	}
}

// CooperationCategory 是合作事项所属的领域。
type CooperationCategory string

const (
	CatCulture   CooperationCategory = "culture"
	CatTourism   CooperationCategory = "tourism"
	CatEducation CooperationCategory = "education"
	CatYouth     CooperationCategory = "youth"
	CatEconomy   CooperationCategory = "economy"
	CatHealth    CooperationCategory = "health"
	CatOther     CooperationCategory = "other"
)

// Valid 报告合作类别是否受控。
func (c CooperationCategory) Valid() bool {
	switch c {
	case CatCulture, CatTourism, CatEducation, CatYouth, CatEconomy, CatHealth, CatOther:
		return true
	default:
		return false
	}
}

// PlanStatus 是路线图/交往计划的状态。
type PlanStatus string

const (
	PlanProposed   PlanStatus = "proposed"   // 上级提出，尚未约束下级
	PlanBinding    PlanStatus = "binding"    // 已生效，可约束下级计划
	PlanSuperseded PlanStatus = "superseded" // 被新版本替代
	PlanWithdrawn  PlanStatus = "withdrawn"  // 撤回
)

func (s PlanStatus) Valid() bool {
	switch s {
	case PlanProposed, PlanBinding, PlanSuperseded, PlanWithdrawn:
		return true
	default:
		return false
	}
}

// CommitmentStatus 是承诺事项的执行状态。
type CommitmentStatus string

const (
	CommitOpen       CommitmentStatus = "open"
	CommitInProgress CommitmentStatus = "in_progress"
	CommitFulfilled  CommitmentStatus = "fulfilled"
	CommitOverdue    CommitmentStatus = "overdue"
	CommitCancelled  CommitmentStatus = "cancelled"
)

func (s CommitmentStatus) Valid() bool {
	switch s {
	case CommitOpen, CommitInProgress, CommitFulfilled, CommitOverdue, CommitCancelled:
		return true
	default:
		return false
	}
}
