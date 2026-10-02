package referralstats

import (
	"math"
	"testing"
)

func mustSubmit(t *testing.T, s *Store, id, jobID int64, email string, submittedAt int64, role Role) {
	t.Helper()
	if err := s.SubmitReferral(id, jobID, email, submittedAt, role); err != nil {
		t.Fatalf("SubmitReferral(%d) failed: %v", id, err)
	}
}

func mustHire(t *testing.T, s *Store, id, hiredAt int64) {
	t.Helper()
	if err := s.RecordHire(id, hiredAt); err != nil {
		t.Fatalf("RecordHire(%d) failed: %v", id, err)
	}
}

func mustDelete(t *testing.T, s *Store, id int64) {
	t.Helper()
	if err := s.DeleteReferral(id); err != nil {
		t.Fatalf("DeleteReferral(%d) failed: %v", id, err)
	}
}

func mustReport(t *testing.T, s *Store, start, end int64) []RoleStats {
	t.Helper()
	stats, err := s.Report(start, end)
	if err != nil {
		t.Fatalf("Report(%d, %d) failed: %v", start, end, err)
	}
	return stats
}

func findRole(t *testing.T, stats []RoleStats, role Role) RoleStats {
	t.Helper()
	for _, st := range stats {
		if st.Role == role {
			return st
		}
	}
	t.Fatalf("role %s not found in report %+v", role, stats)
	return RoleStats{}
}

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// 同秒提交时按编号择先；补录更早记录后来源角色与查询区间归属变化。
func TestTieBreakByIDAndBackfill(t *testing.T) {
	s := NewStore()
	const job = 1
	const email = "cand@example.com"

	// 同秒提交，编号较小者优先作为归属推荐。
	mustSubmit(t, s, 20, job, email, 1000, RoleAlumni)
	mustSubmit(t, s, 10, job, email, 1000, RoleEmployee)

	stats := mustReport(t, s, 0, 2000)
	if len(stats) != 1 {
		t.Fatalf("expected 1 role row, got %+v", stats)
	}
	emp := findRole(t, stats, RoleEmployee)
	if emp.Candidates != 1 || emp.Hires != 0 {
		t.Fatalf("expected employee candidates=1 hires=0, got %+v", emp)
	}

	// 补录一条更早的推荐，来源角色变为 ambassador，入组时间前移。
	mustSubmit(t, s, 5, job, email, 500, RoleAmbassador)

	stats = mustReport(t, s, 0, 2000)
	if len(stats) != 1 {
		t.Fatalf("expected 1 role row after backfill, got %+v", stats)
	}
	amb := findRole(t, stats, RoleAmbassador)
	if amb.Candidates != 1 {
		t.Fatalf("expected ambassador candidates=1, got %+v", amb)
	}

	// 入组时间变为 500，原区间 [1000, 2000) 不再包含该组。
	stats = mustReport(t, s, 1000, 2000)
	if len(stats) != 0 {
		t.Fatalf("expected empty report for [1000,2000), got %+v", stats)
	}
	// 新区间 [0, 1000) 包含该组。
	stats = mustReport(t, s, 0, 1000)
	amb = findRole(t, stats, RoleAmbassador)
	if amb.Candidates != 1 {
		t.Fatalf("expected ambassador candidates=1 in [0,1000), got %+v", amb)
	}
}

// 归属推荐与录用推荐不同；连续删除归属记录、最早录用记录后的重新归属与耗时变化。
func TestReattributionAfterDeletes(t *testing.T) {
	s := NewStore()
	const job = 7
	const email = "x@example.com"

	// id=1 归属（employee, t=100），id=2 录用（alumni, t=200, hired=400），
	// id=3 录用更晚（ambassador, t=300, hired=500）。
	mustSubmit(t, s, 1, job, email, 100, RoleEmployee)
	mustSubmit(t, s, 2, job, email, 200, RoleAlumni)
	mustSubmit(t, s, 3, job, email, 300, RoleAmbassador)
	mustHire(t, s, 2, 400)
	mustHire(t, s, 3, 500)

	// 初始：入组 100，来源 employee，有效录用 400，耗时 300。
	stats := mustReport(t, s, 0, 10000)
	emp := findRole(t, stats, RoleEmployee)
	if emp.Candidates != 1 || emp.Hires != 1 || !almostEqual(emp.Conversion, 1.0) {
		t.Fatalf("unexpected initial stats: %+v", emp)
	}
	if !almostEqual(emp.AvgDaysToHire, 300) {
		t.Fatalf("expected avg duration 300, got %v", emp.AvgDaysToHire)
	}

	// 删除归属推荐 id=1：重新归属到 id=2（alumni, t=200），耗时变为 400-200=200。
	mustDelete(t, s, 1)
	stats = mustReport(t, s, 0, 10000)
	if len(stats) != 1 {
		t.Fatalf("expected only alumni row, got %+v", stats)
	}
	al := findRole(t, stats, RoleAlumni)
	if al.Candidates != 1 || al.Hires != 1 || !almostEqual(al.AvgDaysToHire, 200) {
		t.Fatalf("unexpected stats after deleting attribution: %+v", al)
	}

	// 删除最早录用记录 id=2：归属移到 id=3（ambassador, t=300），
	// 有效录用时间变为 500，耗时 500-300=200。
	mustDelete(t, s, 2)
	stats = mustReport(t, s, 0, 10000)
	if len(stats) != 1 {
		t.Fatalf("expected only ambassador row, got %+v", stats)
	}
	amb := findRole(t, stats, RoleAmbassador)
	if amb.Candidates != 1 || amb.Hires != 1 || !almostEqual(amb.AvgDaysToHire, 200) {
		t.Fatalf("unexpected stats after deleting earliest hire: %+v", amb)
	}

	// 删除最后一条 id=3：组消失，报表为空。
	mustDelete(t, s, 3)
	stats = mustReport(t, s, 0, 10000)
	if len(stats) != 0 {
		t.Fatalf("expected empty report after deleting all, got %+v", stats)
	}

	// 同一职位与邮箱的新推荐重新形成候选组。
	mustSubmit(t, s, 9, job, email, 800, RoleHRAdmin)
	stats = mustReport(t, s, 0, 10000)
	hr := findRole(t, stats, RoleHRAdmin)
	if hr.Candidates != 1 || hr.Hires != 0 {
		t.Fatalf("expected re-formed group under hr_admin, got %+v", hr)
	}
}

// 删除最早录用记录后，若组内再无录用，该组重新成为未录用。
func TestDeleteOnlyHireRevertsToUnhired(t *testing.T) {
	s := NewStore()
	mustSubmit(t, s, 1, 1, "a@example.com", 100, RoleEmployee)
	mustSubmit(t, s, 2, 1, "a@example.com", 200, RoleAlumni)
	mustHire(t, s, 2, 300)

	stats := mustReport(t, s, 0, 1000)
	emp := findRole(t, stats, RoleEmployee)
	if emp.Hires != 1 {
		t.Fatalf("expected hired, got %+v", emp)
	}

	mustDelete(t, s, 2)
	stats = mustReport(t, s, 0, 1000)
	emp = findRole(t, stats, RoleEmployee)
	if emp.Candidates != 1 || emp.Hires != 0 || emp.Conversion != 0 || emp.AvgDaysToHire != 0 {
		t.Fatalf("expected unhired after deleting only hire, got %+v", emp)
	}
}

// 报表区间边界：包含起点、不包含终点；录用时间不受区间限制。
func TestReportRangeBoundaries(t *testing.T) {
	s := NewStore()
	mustSubmit(t, s, 1, 1, "a@example.com", 100, RoleEmployee)
	mustSubmit(t, s, 2, 1, "b@example.com", 200, RoleAlumni)
	// 录用时间远在查询区间之外，仍计入转化。
	mustHire(t, s, 1, 999999)

	stats := mustReport(t, s, 100, 200)
	if len(stats) != 1 {
		t.Fatalf("expected only employee in [100,200), got %+v", stats)
	}
	emp := findRole(t, stats, RoleEmployee)
	if emp.Candidates != 1 || emp.Hires != 1 || !almostEqual(emp.AvgDaysToHire, 999899) {
		t.Fatalf("unexpected boundary stats: %+v", emp)
	}

	if _, err := s.Report(200, 100); err != ErrInvalidRange {
		t.Fatalf("expected ErrInvalidRange, got %v", err)
	}
}

// 输入校验：重复编号、非法角色、重复录用、录用早于提交、删除不存在或已删除的推荐。
func TestValidation(t *testing.T) {
	s := NewStore()
	mustSubmit(t, s, 1, 1, "a@example.com", 100, RoleEmployee)

	if err := s.SubmitReferral(1, 1, "b@example.com", 100, RoleAlumni); err != ErrDuplicateID {
		t.Fatalf("expected ErrDuplicateID, got %v", err)
	}
	if err := s.SubmitReferral(2, 1, "b@example.com", 100, "ceo"); err != ErrInvalidRole {
		t.Fatalf("expected ErrInvalidRole, got %v", err)
	}
	if err := s.RecordHire(1, 50); err != ErrHireBeforeSubmit {
		t.Fatalf("expected ErrHireBeforeSubmit, got %v", err)
	}
	if err := s.RecordHire(99, 100); err != ErrReferralNotFound {
		t.Fatalf("expected ErrReferralNotFound, got %v", err)
	}
	mustHire(t, s, 1, 100)
	if err := s.RecordHire(1, 200); err != ErrAlreadyHired {
		t.Fatalf("expected ErrAlreadyHired, got %v", err)
	}
	mustDelete(t, s, 1)
	if err := s.DeleteReferral(1); err != ErrReferralNotFound {
		t.Fatalf("expected ErrReferralNotFound on second delete, got %v", err)
	}
	if err := s.RecordHire(1, 300); err != ErrReferralNotFound {
		t.Fatalf("expected ErrReferralNotFound for deleted referral, got %v", err)
	}
}

// 平均耗时只覆盖已录用组；转化率按候选组去重统计。
func TestAverageOnlyOverHiredGroups(t *testing.T) {
	s := NewStore()
	// 两个 employee 候选组：一个录用（耗时 100），一个未录用。
	mustSubmit(t, s, 1, 1, "a@example.com", 100, RoleEmployee)
	mustHire(t, s, 1, 200)
	mustSubmit(t, s, 2, 1, "b@example.com", 100, RoleEmployee)
	// 同组重复推荐不重复计为候选人。
	mustSubmit(t, s, 3, 1, "a@example.com", 150, RoleAlumni)

	stats := mustReport(t, s, 0, 1000)
	emp := findRole(t, stats, RoleEmployee)
	if emp.Candidates != 2 || emp.Hires != 1 || !almostEqual(emp.Conversion, 0.5) {
		t.Fatalf("unexpected stats: %+v", emp)
	}
	if !almostEqual(emp.AvgDaysToHire, 100) {
		t.Fatalf("expected avg 100 over hired groups only, got %v", emp.AvgDaysToHire)
	}
}
