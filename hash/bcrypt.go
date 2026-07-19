package hash

import "golang.org/x/crypto/bcrypt"

// --- Bcrypt 密码哈希 ---

// BcryptCost bcrypt 计算成本，值越大越安全但越慢。
// 4~31 之间，推荐值 10（默认）或 12。
const (
	BcryptCostMin     = bcrypt.MinCost     // 4
	BcryptCostMax     = bcrypt.MaxCost     // 31
	BcryptCostDefault = bcrypt.DefaultCost // 10
)

// BcryptHash 使用 bcrypt 对密码进行哈希，返回哈希字符串。
// cost 为计算成本（4~31），推荐 10 或 12。
// 如果 cost < 0，使用默认值 BcryptCostDefault。
func BcryptHash(password string, cost int) (string, error) {
	if cost < 0 {
		cost = BcryptCostDefault
	}
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// BcryptHashDefault 使用默认成本（10）对密码进行 bcrypt 哈希。
func BcryptHashDefault(password string) (string, error) {
	return BcryptHash(password, BcryptCostDefault)
}

// BcryptCompare 将 bcrypt 哈希与明文密码进行比较。
// 返回 nil 表示匹配，返回非 nil 表示不匹配或哈希格式错误。
func BcryptCompare(hashedPassword, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}

// BcryptMatch 检查 bcrypt 哈希是否与明文密码匹配。
// 返回 true 表示匹配。
func BcryptMatch(hashedPassword, password string) bool {
	return BcryptCompare(hashedPassword, password) == nil
}

// BcryptIsHashed 检查字符串是否为 bcrypt 哈希格式。
// bcrypt 哈希以 "$2a$"、"$2b$" 或 "$2y$" 开头。
func BcryptIsHashed(s string) bool {
	if len(s) < 7 {
		return false
	}
	return s[:4] == "$2a$" || s[:4] == "$2b$" || s[:4] == "$2y$"
}
