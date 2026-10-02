package referral

import (
	"errors"
	"testing"
)

func mustSubmit(t *testing.T, tr *Tracker, id, jobID int64, email string, at int64, role Role) {
	t.Helper()
	if err := tr.SubmitReferral(id, jobID, email, at, role); err != nil {
		t.Fatalf("SubmitReferral(%d) failed: %v", id, err)
	}
}

func mustHire(t *testing.T, tr *Tracker, id, at int64) {
	t.Helper()
	if err := tr.RecordHire(id, at); err != nil {
		t.Fatalf("RecordHire(%d) failed: %v", id, err)
	}
}

func mustDelete(t *testing.T, tr *Tracker, id int64) {
	t.Helper()
	if err := tr.DeleteReferral(id); err != nil {
		t.Fatalf("DeleteReferral(%d) failed: %v", id, err)
	}
}

func mustReport(t *testing.T, tr *Tracker, start, end int64) []RoleStats {
	t.Helper()
	stats, err := tr.Report(start, end)
	if err != nil {
		t.Fatalf("Report(%d, %d) failed: %v", start, end, err)
	}
	if len(stats) != 4 {
		t.Fatalf("expected 4 role rows, got %d", len(stats))
	}
	return stats
}

func findRole(t *testing.T, stats []RoleStats, role Role) RoleStats {
	t.Helper()
	for _, s := range stats {
		if s.Role == role {
			return s
		}
	}
	t.Fatalf("role %s not in report", role)
	return RoleStats{}
}

func almostEq(a, b float64) bool {
	d := a - b
	return d > -1e-9 && d < 1e-9
}

// 同秒提交时按编号择先；补录更早记录后来源角色与查询区间归属变化。
func TestTieBreakByIDAndBackfill(t *testing.T) {
	tr := NewTracker()
	// 同一职位+邮箱，同秒提交三条，编号最小者（id=5, employee）为归属推荐。
	mustSubmit(t, tr, 9, 100, "a@x.com", 1000, RoleAmbassador)
	mustSubmit(t, tr, 5, 100, "a@x.com", 1000, RoleEmployee)
	mustSubmit(t, tr, 7, 100, "a@x.com", 1000, RoleAlumni)

	stats := mustReport(t, tr, 0, 2000)
	emp := findRole(t, stats, RoleEmployee)
	if emp.Candidates != 1 {
		t.Fatalf("expected employee to own the group, got %+v", stats)
	}
	if findRole(t, stats, RoleAlumni).Candidates != 0 ||
		findRole(t, stats, RoleAmbassador).Candidates != 0 {
		t.Fatalf("other roles must not count the group, got %+v", stats)
	}

	// 补录一条更早的推荐（id=1, hr_admin, t=500），归属改变。
	mustSubmit(t, tr, 1, 100, "a@x.com", 500, RoleHRAdmin)

	stats = mustReport(t, tr, 0, 2000)
	if findRole(t, stats, RoleHRAdmin).Candidates != 1 ||
		findRole(t, stats, RoleEmployee).Candidates != 0 {
		t.Fatalf("backfilled hr_admin referral should own the group, got %+v", stats)
	}

	// 入组时间从 1000 变为 500：旧区间 [1000,2000) 不再包含该组，新区间 [0,1000) 包含。
	stats = mustReport(t, tr, 1000, 2000)
	for _, s := range stats {
		if s.Candidates != 0 {
			t.Fatalf("range [1000,2000) should be empty after backfill, got %+v", stats)
		}
	}
	stats = mustReport(t, tr, 0, 1000)
	if findRole(t, stats, RoleHRAdmin).Candidates != 1 {
		t.Fatalf("range [0,1000) should contain the group, got %+v", stats)
	}
}

// 归属推荐与录用推荐不同；连续删除归属记录、最早录用记录后的重新归属与耗时变化。
func TestDeleteAttributionAndEarliestHire(t *testing.T) {
	tr := NewTracker()
	// 组内三条：id=1 employee t=100（归属），id=2 alumni t=200，id=3 ambassador t=300。
	mustSubmit(t, tr, 1, 100, "b@x.com", 100, RoleEmployee)
	mustSubmit(t, tr, 2, 100, "b@x.com", 200, RoleAlumni)
	mustSubmit(t, tr, 3, 100, "b@x.com", 300, RoleAmbassador)
	// 录用登记在非归属推荐上：id=2 在 t=500 录用，id=3 在 t=400 录用。
	mustHire(t, tr, 2, 500)
	mustHire(t, tr, 3, 400)

	// 初始：归属 employee，入组 t=100；有效录用取最早 t=400；耗时 300。
	stats := mustReport(t, tr, 0, 1000)
	emp := findRole(t, stats, RoleEmployee)
	if emp.Candidates != 1 || emp.Hires != 1 ||
		!almostEq(emp.ConversionRate, 1.0) || !almostEq(emp.AvgTimeToHire, 300) {
		t.Fatalf("initial stats wrong: %+v", emp)
	}

	// 删除归属推荐 id=1：重新归属到 id=2 (alumni, t=200)，耗时变为 400-200=200。
	mustDelete(t, tr, 1)
	stats = mustReport(t, tr, 0, 1000)
	if findRole(t, stats, RoleEmployee).Candidates != 0 {
		t.Fatalf("employee should lose the group, got %+v", stats)
	}
	al := findRole(t, stats, RoleAlumni)
	if al.Candidates != 1 || al.Hires != 1 || !almostEq(al.AvgTimeToHire, 200) {
		t.Fatalf("alumni should own the group with tth=200, got %+v", al)
	}

	// 删除最早录用记录 id=3 (t=400)：有效录用时间变为 id=2 的 t=500，耗时 500-200=300。
	mustDelete(t, tr, 3)
	stats = mustReport(t, tr, 0, 1000)
	al = findRole(t, stats, RoleAlumni)
	if al.Candidates != 1 || al.Hires != 1 || !almostEq(al.AvgTimeToHire, 300) {
		t.Fatalf("after deleting earliest hire, tth should be 300, got %+v", al)
	}

	// 再删除 id=2（归属且唯一剩余录用）：组归属到 id=3？id=3 已删除——
	// 组内只剩已删除记录，组消失。
	mustDelete(t, tr, 2)
	stats = mustReport(t, tr, 0, 1000)
	for _, s := range stats {
		if s.Candidates != 0 || s.Hires != 0 {
			t.Fatalf("group should disappear after all referrals deleted, got %+v", stats)
		}
	}
}

// 删除唯一录用记录使组重新成为未录用。
func TestDeleteOnlyHireRevertsToUnhired(t *testing.T) {
	tr := NewTracker()
	mustSubmit(t, tr, 1, 100, "c@x.com", 100, RoleEmployee)
	mustSubmit(t, tr, 2, 100, "c@x.com", 200, RoleAlumni)
	mustHire(t, tr, 1, 300)

	stats := mustReport(t, tr, 0, 1000)
	if findRole(t, stats, RoleEmployee).Hires != 1 {
		t.Fatalf("expected 1 hire, got %+v", stats)
	}

	mustDelete(t, tr, 1) // 删除带录用的归属推荐
	stats = mustReport(t, tr, 0, 1000)
	al := findRole(t, stats, RoleAlumni)
	if al.Candidates != 1 || al.Hires != 0 ||
		!almostEq(al.ConversionRate, 0) || !almostEq(al.AvgTimeToHire, 0) {
		t.Fatalf("group should revert to unhired under alumni, got %+v", al)
	}
}

// 组内全部删除后，同职位+邮箱的新推荐重新形成候选组。
func TestGroupReformsAfterFullDeletion(t *testing.T) {
	tr := NewTracker()
	mustSubmit(t, tr, 1, 100, "d@x.com", 100, RoleEmployee)
	mustDelete(t, tr, 1)

	stats := mustReport(t, tr, 0, 1000)
	for _, s := range stats {
		if s.Candidates != 0 {
			t.Fatalf("expected empty report, got %+v", stats)
		}
	}

	mustSubmit(t, tr, 2, 100, "d@x.com", 500, RoleAmbassador)
	mustHire(t, tr, 2, 800)
	stats = mustReport(t, tr, 0, 1000)
	amb := findRole(t, stats, RoleAmbassador)
	if amb.Candidates != 1 || amb.Hires != 1 || !almostEq(amb.AvgTimeToHire, 300) {
		t.Fatalf("reformed group wrong: %+v", amb)
	}
}

// 报表区间边界：包含起点、不包含终点；录用时间不受区间限制。
func TestReportRangeBoundaries(t *testing.T) {
	tr := NewTracker()
	mustSubmit(t, tr, 1, 100, "e1@x.com", 100, RoleEmployee)
	mustSubmit(t, tr, 2, 100, "e2@x.com", 200, RoleEmployee)
	mustHire(t, tr, 1, 5000) // 录用时间远在查询区间之外

	stats := mustReport(t, tr, 100, 200)
	emp := findRole(t, stats, RoleEmployee)
	if emp.Candidates != 1 || emp.Hires != 1 || !almostEq(emp.AvgTimeToHire, 4900) {
		t.Fatalf("[100,200) should include only t=100 group, hire time not range-limited: %+v", emp)
	}

	stats = mustReport(t, tr, 200, 300)
	emp = findRole(t, stats, RoleEmployee)
	if emp.Candidates != 1 || emp.Hires != 0 {
		t.Fatalf("[200,300) should include only t=200 group: %+v", emp)
	}
}

// 不同职位或不同邮箱属于不同候选组，分别计数。
func TestDistinctGroupsCountedSeparately(t *testing.T) {
	tr := NewTracker()
	mustSubmit(t, tr, 1, 100, "f@x.com", 100, RoleEmployee)
	mustSubmit(t, tr, 2, 200, "f@x.com", 100, RoleEmployee) // 不同职位
	mustSubmit(t, tr, 3, 100, "g@x.com", 100, RoleEmployee) // 不同邮箱

	stats := mustReport(t, tr, 0, 1000)
	emp := findRole(t, stats, RoleEmployee)
	if emp.Candidates != 3 {
		t.Fatalf("expected 3 distinct groups, got %+v", emp)
	}
}

// 平均值只覆盖已录用组；转化率与多组平均耗时。
func TestAveragesCoverHiredOnly(t *testing.T) {
	tr := NewTracker()
	mustSubmit(t, tr, 1, 100, "h1@x.com", 0, RoleAlumni)
	mustSubmit(t, tr, 2, 100, "h2@x.com", 0, RoleAlumni)
	mustSubmit(t, tr, 3, 100, "h3@x.com", 0, RoleAlumni)
	mustHire(t, tr, 1, 100) // 耗时 100
	mustHire(t, tr, 2, 300) // 耗时 300；h3 未录用

	stats := mustReport(t, tr, 0, 1000)
	al := findRole(t, stats, RoleAlumni)
	if al.Candidates != 3 || al.Hires != 2 ||
		!almostEq(al.ConversionRate, 2.0/3.0) || !almostEq(al.AvgTimeToHire, 200) {
		t.Fatalf("wrong aggregate stats: %+v", al)
	}
}

// 错误处理：重复编号、重复录用、录用早于提交、删除/录用不存在的推荐、非法区间。
func TestErrorCases(t *testing.T) {
	tr := NewTracker()
	mustSubmit(t, tr, 1, 100, "i@x.com", 100, RoleEmployee)

	if err := tr.SubmitReferral(1, 100, "i@x.com", 100, RoleEmployee); !errors.Is(err, ErrReferralExists) {
		t.Fatalf("expected ErrReferralExists, got %v", err)
	}
	if err := tr.RecordHire(1, 99); !errors.Is(err, ErrHireBeforeSubmit) {
		t.Fatalf("expected ErrHireBeforeSubmit, got %v", err)
	}
	mustHire(t, tr, 1, 100) // 等于提交时间，允许
	if err := tr.RecordHire(1, 200); !errors.Is(err, ErrAlreadyHired) {
		t.Fatalf("expected ErrAlreadyHired, got %v", err)
	}
	if err := tr.RecordHire(999, 100); !errors.Is(err, ErrReferralMissing) {
		t.Fatalf("expected ErrReferralMissing, got %v", err)
	}
	if err := tr.DeleteReferral(999); !errors.Is(err, ErrReferralMissing) {
		t.Fatalf("expected ErrReferralMissing, got %v", err)
	}
	mustDelete(t, tr, 1)
	if err := tr.DeleteReferral(1); !errors.Is(err, ErrReferralMissing) {
		t.Fatalf("expected ErrReferralMissing on double delete, got %v", err)
	}
	if err := tr.RecordHire(1, 200); !errors.Is(err, ErrReferralMissing) {
		t.Fatalf("expected ErrReferralMissing on hire of deleted, got %v", err)
	}
	if _, err := tr.Report(200, 100); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("expected ErrInvalidRange, got %v", err)
	}
	if _, err := tr.Report(100, 100); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("expected ErrInvalidRange for empty range, got %v", err)
	}
}
