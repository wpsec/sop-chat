package sharetoken

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	QueryParam       = "shareToken"
	DefaultExpiresIn = 7 * 24 * time.Hour
	defaultSecretKey = "default-secret-key-change-in-production"
)

var (
	ErrExpiredToken = errors.New("share token expired")
	ErrInvalidToken = errors.New("share token invalid")
)

type Claims struct {
	EmployeeName   string `json:"employeeName"`
	ThreadID       string `json:"threadId"`
	CloudAccountID string `json:"cloudAccountId,omitempty"`
	jwt.RegisteredClaims
}

type Manager struct {
	secretKey []byte
	expiresIn time.Duration
}

func NewManager(secretKey string, expiresIn time.Duration) *Manager {
	if secretKey == "" {
		secretKey = os.Getenv("JWT_SECRET_KEY")
		if secretKey == "" {
			secretKey = defaultSecretKey
		}
	}
	if expiresIn <= 0 {
		expiresIn = DefaultExpiresIn
	}

	return &Manager{
		secretKey: []byte("share:" + secretKey),
		expiresIn: expiresIn,
	}
}

func (m *Manager) Generate(employeeName, threadID, cloudAccountID string) (string, time.Time, error) {
	employeeName = strings.TrimSpace(employeeName)
	threadID = strings.TrimSpace(threadID)
	cloudAccountID = strings.TrimSpace(cloudAccountID)
	if employeeName == "" || threadID == "" {
		return "", time.Time{}, fmt.Errorf("employeeName and threadID cannot be empty")
	}

	now := time.Now()
	expiresAt := now.Add(m.expiresIn)
	claims := &Claims{
		EmployeeName:   employeeName,
		ThreadID:       threadID,
		CloudAccountID: cloudAccountID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "sop-chat-share",
			Subject:   threadID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secretKey)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func (m *Manager) Validate(tokenString string) (*Claims, error) {
	tokenString = strings.TrimSpace(tokenString)
	if tokenString == "" {
		return nil, ErrInvalidToken
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return m.secretKey, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, fmt.Errorf("%w: %v", ErrExpiredToken, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func BuildURL(baseURL, employeeName, threadID, token string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	values := url.Values{}
	values.Set(QueryParam, token)
	return fmt.Sprintf("%s/#/share/%s/%s?%s",
		baseURL,
		url.PathEscape(strings.TrimSpace(employeeName)),
		url.PathEscape(strings.TrimSpace(threadID)),
		values.Encode(),
	)
}
