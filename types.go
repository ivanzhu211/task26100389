package referral

// Role 推荐人提交推荐时的角色，固定不变。
type Role string

const (
	RoleHRAdmin    Role = "hr_admin"
	RoleEmployee   Role = "employee"
	RoleAlumni     Role = "alumni"
	RoleAmbassador Role = "ambassador"
)

// roleOrder 报表中角色的固定输出顺序。
var roleOrder = []Role{RoleHRAdmin, RoleEmployee, RoleAlumni, RoleAmbassador}

// referral 单条推荐记录（内部表示）。
type referral struct {
	id          int64
	jobID       int64
	email       string
	submittedAt int64 // 秒级 UTC
	role        Role
	hired       bool
	hiredAt     int64 // 秒级 UTC，仅 hired 为 true 时有效
	deleted     bool
}

// groupKey 候选组键：职位 + 已规范化邮箱。
type groupKey struct {
	jobID int64
	email string
}

// RoleStats 单一来源角色的统计结果。
type RoleStats struct {
	Role           Role
	Candidates     int     // 候选组数（去重后）
	Hires          int     // 已录用候选组数
	ConversionRate float64 // Hires / Candidates，无候选人时为 0
	AvgTimeToHire  float64 // 平均录用耗时（秒），仅覆盖已录用组，无录用时为 0
}
