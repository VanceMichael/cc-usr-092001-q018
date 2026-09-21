package event

// payloadFactories 把事件类型映射到其类型化载荷的零值构造器。
var payloadFactories = map[string]func() any{
	TypeEntityRegistered:      func() any { return &RegisterEntityPayload{} },
	TypeEntityRenamed:         func() any { return &RenameEntityPayload{} },
	TypeEntityMerged:          func() any { return &MergeEntitiesPayload{} },
	TypeEntityAliasAdded:      func() any { return &AddAliasPayload{} },

	TypeRelationProposed:      func() any { return &ProposeRelationPayload{} },
	TypeApprovalRecorded:      func() any { return &RecordApprovalPayload{} },
	TypeTextRecorded:          func() any { return &RecordTextPayload{} },
	TypeRelationCityConfirmed: func() any { return &ConfirmRelationCityPayload{} },
	TypeRelationConcluded:     func() any { return &ConcludeRelationPayload{} },
	TypeRelationSuspended:     func() any { return &RelationStatusPayload{} },
	TypeRelationResumed:       func() any { return &RelationStatusPayload{} },
	TypeRelationTerminated:    func() any { return &RelationStatusPayload{} },

	TypePlanRegistered:        func() any { return &RegisterPlanPayload{} },
	TypePlanBound:             func() any { return &BindPlanPayload{} },
	TypePlanSuperseded:        func() any { return &SupersedePlanPayload{} },
	TypePlanWithdrawn:         func() any { return &PlanRefPayload{} },
	TypePlanCityConfirmed:     func() any { return &PlanRefPayload{} },

	TypeCommitmentRegistered:  func() any { return &RegisterCommitmentPayload{} },
	TypeCommitmentProgressed:  func() any { return &ProgressCommitmentPayload{} },
	TypeCommitmentFulfilled:   func() any { return &FulfillCommitmentPayload{} },
	TypeCommitmentCancelled:   func() any { return &CancelCommitmentPayload{} },

	TypeActivityRecorded:      func() any { return &RecordActivityPayload{} },
}
