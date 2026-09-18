package jwt

import "time"

var _jwt *JWT

func Init(secretKey string) {
	_jwt = NewJWT(secretKey)
}

func GenToken(userId string, username string, expirationTime time.Duration) (string, error) {
	claims := CustomClaims{
		UserId:   userId,
		Username: username,
	}
	return _jwt.GenerateToken(claims, expirationTime)
}

func ParseToken(tokenString string) (*CustomClaims, error) {
	return _jwt.ParseToken(tokenString)
}
