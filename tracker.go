package referral

import "errors"

var (
	ErrReferralExists   = errors.New("referral: duplicate referral id")
	ErrReferralMissing  = errors.New("referral: referral id not found")
	ErrAlreadyHired     = errors.New("referral: hire already recorded for this referral")
	ErrHireBeforeSubmit = errors.New("referral: hire time earlier than submit time")
	ErrInvalidRange     = errors.New("referral: report start must be before end")
)

// Tracker 进程内推荐转化统计器，非并发安全；如需并发请由调用方加锁。
type Tracker struct {
	byID map[int64]*referral
}

// NewTracker 创建空的统计器。
func NewTracker() *Tracker {
	return &Tracker{byID: make(map[int64]*referral)}
}

// SubmitReferral 提交一条推荐。id 全局唯一；submittedAt 为秒级 UTC，允许补录过去时间。
func (t *Tracker) SubmitReferral(id, jobID int64, email string, submittedAt int64, role Role) error {
	if _, ok := t.byID[id]; ok {
		return ErrReferralExists
	}
	t.byID[id] = &referral{
		id:          id,
		jobID:       jobID,
		email:       email,
		submittedAt: submittedAt,
		role:        role,
	}
	return nil
}

// RecordHire 为某条推荐登记录用。每条推荐最多登记一次，录用时间不早于该条提交时间。
// 已软删除的推荐不允许登记录用。
func (t *Tracker) RecordHire(referralID, hiredAt int64) error {
	r, ok := t.byID[referralID]
	if !ok || r.deleted {
		return ErrReferralMissing
	}
	if r.hired {
		return ErrAlreadyHired
	}
	if hiredAt < r.submittedAt {
		return ErrHireBeforeSubmit
	}
	r.hired = true
	r.hiredAt = hiredAt
	return nil
}

// DeleteReferral 软删除一条推荐，其提交与录用事实不再参与统计。
func (t *Tracker) DeleteReferral(referralID int64) error {
	r, ok := t.byID[referralID]
	if !ok || r.deleted {
		return ErrReferralMissing
	}
	r.deleted = true
	return nil
}

// groupView 候选组在某个时刻的汇总视图。
type groupView struct {
	entryAt    int64 // 入组时间：归属推荐的提交时间
	role       Role  // 来源角色：归属推荐的角色
	hired      bool
	hiredAt    int64 // 有效录用时间：组内未删除推荐中最早录用时间
	timeToHire int64 // hiredAt - entryAt
}

// summarize 计算候选组当前视图。ok 为 false 表示组内已无未删除推荐。
func summarize(group []*referral) (groupView, bool) {
	var attr *referral
	var hired bool
	var hiredAt int64
	for _, r := range group {
		if r.deleted {
			continue
		}
		if attr == nil || r.submittedAt < attr.submittedAt ||
			(r.submittedAt == attr.submittedAt && r.id < attr.id) {
			attr = r
		}
		if r.hired && (!hired || r.hiredAt < hiredAt) {
			hired = true
			hiredAt = r.hiredAt
		}
	}
	if attr == nil {
		return groupView{}, false
	}
	v := groupView{entryAt: attr.submittedAt, role: attr.role, hired: hired, hiredAt: hiredAt}
	if hired {
		v.timeToHire = hiredAt - attr.submittedAt
	}
	return v, true
}

// groups 按候选组归集当前全部推荐（含已删除，删除判断在 summarize 中做）。
func (t *Tracker) groups() map[groupKey][]*referral {
	groups := make(map[groupKey][]*referral)
	for _, r := range t.byID {
		k := groupKey{jobID: r.jobID, email: r.email}
		groups[k] = append(groups[k], r)
	}
	return groups
}

// Report 按入组时间筛选 [start, end)，按来源角色统计。录用时间不受区间限制。
// 返回固定包含全部四种角色，顺序为 hr_admin、employee、alumni、ambassador。
func (t *Tracker) Report(start, end int64) ([]RoleStats, error) {
	if start >= end {
		return nil, ErrInvalidRange
	}
	stats := make(map[Role]*RoleStats, len(roleOrder))
	out := make([]RoleStats, 0, len(roleOrder))
	for _, role := range roleOrder {
		out = append(out, RoleStats{Role: role})
		stats[role] = &out[len(out)-1]
	}
	var hireTimeSum map[Role]int64 = make(map[Role]int64)
	for _, group := range t.groups() {
		v, ok := summarize(group)
		if !ok {
			continue
		}
		if v.entryAt < start || v.entryAt >= end {
			continue
		}
		s := stats[v.role]
		s.Candidates++
		if v.hired {
			s.Hires++
			hireTimeSum[v.role] += v.timeToHire
		}
	}
	for i := range out {
		s := &out[i]
		if s.Candidates > 0 {
			s.ConversionRate = float64(s.Hires) / float64(s.Candidates)
		}
		if s.Hires > 0 {
			s.AvgTimeToHire = float64(hireTimeSum[s.Role]) / float64(s.Hires)
		}
	}
	return out, nil
}
