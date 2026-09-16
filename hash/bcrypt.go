package hash

import "golang.org/x/crypto/bcrypt"

// --- Bcrypt password hashing ---

// BcryptCost is the bcrypt computation cost: a larger value is more secure but slower.
// It ranges from 4 to 31; the recommended values are 10 (default) or 12.
const (
	BcryptCostMin     = bcrypt.MinCost     // 4
	BcryptCostMax     = bcrypt.MaxCost     // 31
	BcryptCostDefault = bcrypt.DefaultCost // 10
)

// BcryptHash hashes a password with bcrypt and returns the hash string.
// cost is the computation cost (4-31); 10 or 12 are recommended.
// If cost < 0, the default value BcryptCostDefault is used.
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

// BcryptHashDefault hashes a password with bcrypt using the default cost (10).
func BcryptHashDefault(password string) (string, error) {
	return BcryptHash(password, BcryptCostDefault)
}

// BcryptCompare compares a bcrypt hash against a plaintext password.
// A nil return means a match; a non-nil return means a mismatch or a malformed hash.
func BcryptCompare(hashedPassword, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}

// BcryptMatch reports whether a bcrypt hash matches the plaintext password.
// It returns true on a match.
func BcryptMatch(hashedPassword, password string) bool {
	return BcryptCompare(hashedPassword, password) == nil
}

// BcryptIsHashed reports whether a string is in bcrypt hash format.
// bcrypt hashes start with "$2a$", "$2b$" or "$2y$".
func BcryptIsHashed(s string) bool {
	if len(s) < 7 {
		return false
	}
	return s[:4] == "$2a$" || s[:4] == "$2b$" || s[:4] == "$2y$"
}
