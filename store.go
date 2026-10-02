package referralstats

// Store 保存进程内的全部推荐数据。
type Store struct {
	referrals map[int64]*referral
}

func NewStore() *Store {
	return &Store{referrals: make(map[int64]*referral)}
}

// SubmitReferral 提交一条推荐。id 必须唯一，允许补录过去的提交时间。
func (s *Store) SubmitReferral(id, jobID int64, email string, submittedAt int64, role Role) error {
	switch role {
	case RoleHRAdmin, RoleEmployee, RoleAlumni, RoleAmbassador:
	default:
		return ErrInvalidRole
	}
	if _, ok := s.referrals[id]; ok {
		return ErrDuplicateID
	}
	s.referrals[id] = &referral{
		id:        id,
		jobID:     jobID,
		email:     email,
		submitted: submittedAt,
		role:      role,
	}
	return nil
}

// RecordHire 为某条推荐登记录用。每条推荐最多登记一次，
// 录用时间不早于该条推荐的提交时间。软删除的推荐不可再登记录用。
func (s *Store) RecordHire(id, hiredAt int64) error {
	r, ok := s.referrals[id]
	if !ok || r.deleted {
		return ErrReferralNotFound
	}
	if r.hired {
		return ErrAlreadyHired
	}
	if hiredAt < r.submitted {
		return ErrHireBeforeSubmit
	}
	r.hired = true
	r.hiredAt = hiredAt
	return nil
}

// DeleteReferral 软删除一条推荐，其提交与录用事实不再参与统计。
func (s *Store) DeleteReferral(id int64) error {
	r, ok := s.referrals[id]
	if !ok || r.deleted {
		return ErrReferralNotFound
	}
	r.deleted = true
	return nil
}

// groupKey 标识同一职位与邮箱的候选组。
type groupKey struct {
	jobID int64
	email string
}

// groupView 是一个候选组在当前有效记录下计算出的统计视图。
type groupView struct {
	entry   int64 // 入组时间：归属推荐的提交时间
	attrID  int64 // 归属推荐编号
	role    Role  // 来源角色：归属推荐的角色
	hired   bool
	hiredAt int64 // 有效录用时间：组内最早录用时间
}

// groups 按当前未删除的推荐计算所有候选组的统计视图。
func (s *Store) groups() map[groupKey]*groupView {
	groups := make(map[groupKey]*groupView)
	for _, r := range s.referrals {
		if r.deleted {
			continue
		}
		key := groupKey{jobID: r.jobID, email: r.email}
		g, ok := groups[key]
		if !ok {
			g = &groupView{entry: r.submitted, attrID: r.id, role: r.role}
			groups[key] = g
		}
		// 归属推荐：提交时间最早，时间相同则编号较小者优先。
		if r.submitted < g.entry || (r.submitted == g.entry && r.id < g.attrID) {
			g.entry = r.submitted
			g.attrID = r.id
			g.role = r.role
		}
		if r.hired && (!g.hired || r.hiredAt < g.hiredAt) {
			g.hired = true
			g.hiredAt = r.hiredAt
		}
	}
	return groups
}

// Report 按入组时间 [start, end) 统计各来源角色的转化报表。
// 录用时间不受查询区间限制；平均耗时只统计已录用组。
func (s *Store) Report(start, end int64) ([]RoleStats, error) {
	if end <= start {
		return nil, ErrInvalidRange
	}
	type acc struct {
		candidates int
		hires      int
		totalDur   int64
	}
	accs := make(map[Role]*acc)
	for _, g := range s.groups() {
		if g.entry < start || g.entry >= end {
			continue
		}
		a, ok := accs[g.role]
		if !ok {
			a = &acc{}
			accs[g.role] = a
		}
		a.candidates++
		if g.hired {
			a.hires++
			a.totalDur += g.hiredAt - g.entry
		}
	}
	out := make([]RoleStats, 0, len(accs))
	for role, a := range accs {
		st := RoleStats{
			Role:       role,
			Candidates: a.candidates,
			Hires:      a.hires,
		}
		if a.candidates > 0 {
			st.Conversion = float64(a.hires) / float64(a.candidates)
		}
		if a.hires > 0 {
			st.AvgDaysToHire = float64(a.totalDur) / float64(a.hires)
		}
		out = append(out, st)
	}
	// 固定输出顺序，便于调用方消费与测试断言。
	order := []Role{RoleHRAdmin, RoleEmployee, RoleAlumni, RoleAmbassador}
	sorted := make([]RoleStats, 0, len(out))
	for _, role := range order {
		for _, st := range out {
			if st.Role == role {
				sorted = append(sorted, st)
			}
		}
	}
	return sorted, nil
}
