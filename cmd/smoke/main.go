package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/httpapi"
	"example.com/batch-092001-q018/internal/service"
)

var seq int64

func main() {
	svc := service.New()
	srv := httptest.NewServer(httpapi.New(svc))
	defer srv.Close()

	fail := false
	post := func(typ, sub string, payload any) {
		postExpect(typ, sub, payload, http.StatusCreated, http.StatusOK)
	}
	postExpect := func(typ, sub string, payload any, allow ...int) {
		seq++
		raw, _ := json.Marshal(payload)
		dig, err := event.CanonicalDigest(raw)
		must(err)
		env := event.Envelope{
			SchemaVersion: "1", EventID: fmt.Sprintf("EVT-%06d", seq), Source: "SRC-SMOKE",
			EventType: typ, SubjectRef: sub, OccurredAt: "2026-09-21T10:00:00+08:00",
			SourceSequence: seq, PayloadDigest: dig, Payload: raw,
		}
		body, _ := json.Marshal(env)
		res, err := http.Post(srv.URL+"/v1/events", "application/json", bytes.NewReader(body))
		must(err)
		out, _ := io.ReadAll(res.Body)
		res.Body.Close()
		ok := false
		for _, c := range allow {
			if res.StatusCode == c {
				ok = true
			}
		}
		mark := "OK "
		if !ok {
			mark = "ERR"
			fail = true
		}
		fmt.Printf("%s %d %-28s %s\n", mark, res.StatusCode, typ, string(out))
	}

	lt := func(s string) domain.LocalizedText {
		return domain.LocalizedText{OriginalLanguage: "zh", Original: s}
	}

	// 两国两城
	post(event.TypeEntityRegistered, "CITY-0001", event.RegisterEntityPayload{Ref: "CITY-0001", Level: domain.LevelCity, CurrentName: "苏州", Language: "zh"})
	post(event.TypeEntityRegistered, "CITY-0002", event.RegisterEntityPayload{Ref: "CITY-0002", Level: domain.LevelCity, CurrentName: "威尼斯", Language: "zh"})
	post(event.TypeRelationProposed, "REL-0001", event.ProposeRelationPayload{Ref: "REL-0001", PartyA: "CITY-0001", PartyB: "CITY-0002", Categories: []domain.CooperationCategory{domain.CatCulture, domain.CatTourism}})
	post(event.TypeApprovalRecorded, "REL-0001", event.RecordApprovalPayload{RelationRef: "REL-0001", PartyRef: "CITY-0001", Authority: "市人大", ApprovedAt: "2026-05-01T00:00:00+08:00"})
	post(event.TypeApprovalRecorded, "REL-0001", event.RecordApprovalPayload{RelationRef: "REL-0001", PartyRef: "CITY-0002", Authority: "市政厅", ApprovedAt: "2026-05-02T00:00:00+02:00"})
	post(event.TypeTextRecorded, "REL-0001", event.RecordTextPayload{RelationRef: "REL-0001", Title: lt("友好合作协议"), Signed: true, SignedAt: "2026-05-10T10:00:00+08:00"})
	post(event.TypeRelationCityConfirmed, "REL-0001", event.ConfirmRelationCityPayload{RelationRef: "REL-0001", CityRef: "CITY-0001"})
	post(event.TypeRelationCityConfirmed, "REL-0001", event.ConfirmRelationCityPayload{RelationRef: "REL-0001", CityRef: "CITY-0002"})
	post(event.TypeRelationConcluded, "REL-0001", event.ConcludeRelationPayload{RelationRef: "REL-0001", EffectiveAt: "2026-06-01T00:00:00+08:00"})
	post(event.TypeCommitmentRegistered, "COMM-0001", event.RegisterCommitmentPayload{Ref: "COMM-0001", RelationRef: "REL-0001", Category: domain.CatCulture, Title: lt("园林文化展"), OwnerDepts: map[string]string{"CITY-0001": "DEPT-CULTURE"}, DueAt: "2026-08-01T00:00:00+08:00"})
	post(event.TypeActivityRecorded, "ACT-0001", event.RecordActivityPayload{Ref: "ACT-0001", RelationRef: "REL-0001", CommitmentRef: "COMM-0001", Category: domain.CatCulture, Title: lt("园林文化展开幕"), StartAt: "2026-09-15T09:30:00+08:00", EndAt: "2026-09-15T11:00:00+08:00", TimeZone: "Asia/Shanghai", HostPartyRef: "CITY-0001"})

	// 重复上报同一对：应 409
	postExpect(event.TypeRelationProposed, "REL-0002", event.ProposeRelationPayload{Ref: "REL-0002", PartyA: "CITY-0002", PartyB: "CITY-0001"}, http.StatusConflict)

	// 友城对聚合视图（逾期判定 at=2026-09-21，承诺截止 2026-08-01 已逾期）
	res, err := http.Get(srv.URL + "/v1/relations/REL-0001?at=2026-09-21T00:00:00%2B08:00")
	must(err)
	view, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var parsed map[string]any
	must(json.Unmarshal(view, &parsed))
	sum := parsed["summary"].(map[string]any)
	fmt.Printf("\nPAIR VIEW status=%s overdue=%v total_activities=%v\n",
		parsed["relation"].(map[string]any)["status"], sum["overdue_commitments"], sum["total_activities"])

	// 提议分析：对已缔结双方追加旅游合作
	prop := map[string]any{
		"party_a":    map[string]string{"ref": "CITY-0001"},
		"party_b":    map[string]string{"ref": "CITY-0002"},
		"categories": []string{"tourism"},
	}
	pb, _ := json.Marshal(prop)
	ares, err := http.Post(srv.URL+"/v1/proposals:analyze", "application/json", bytes.NewReader(pb))
	must(err)
	abody, _ := io.ReadAll(ares.Body)
	ares.Body.Close()
	var analysis map[string]any
	must(json.Unmarshal(abody, &analysis))
	fmt.Printf("PROPOSAL recommendation=%v overlaps=%d reusable=%d conflicts=%d\n",
		analysis["recommendation"], len(analysis["overlaps"].([]any)), len(analysis["reusable"].([]any)), len(analysis["conflicts"].([]any)))

	if fail {
		os.Exit(1)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
