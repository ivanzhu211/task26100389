package referralstats

import "errors"

// Role 是推荐人提交推荐时的角色，固定且后续不变。
type Role string

const (
	RoleHRAdmin    Role = "hr_admin"
	RoleEmployee   Role = "employee"
	RoleAlumni     Role = "alumni"
	RoleAmbassador Role = "ambassador"
)

var (
	ErrInvalidRole      = errors.New("invalid role")
	ErrDuplicateID      = errors.New("referral id already exists")
	ErrReferralNotFound = errors.New("referral not found")
	ErrAlreadyHired     = errors.New("referral already has a hire record")
	ErrHireBeforeSubmit = errors.New("hire time is before submit time")
	ErrInvalidRange     = errors.New("invalid report time range")
)

// referral 是库内部保存的一条推荐记录。
type referral struct {
	id        int64
	jobID     int64
	email     string
	submitted int64 // 秒级 UTC
	role      Role
	deleted   bool
	hired     bool
	hiredAt   int64 // 秒级 UTC，仅 hired 为 true 时有效
}

// RoleStats 是报表中单一来源角色的统计行。
type RoleStats struct {
	Role          Role
	Candidates    int     // 候选组数
	Hires         int     // 已录用组数
	Conversion    float64 // Hires / Candidates，无候选人时为 0
	AvgDaysToHire float64 // 平均录用耗时（秒），只统计已录用组
}
