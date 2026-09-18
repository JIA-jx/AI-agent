package auths

import (
	"common/biz"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"model"
	"net/smtp"
	"time"

	"basemodel/cache"
	"basemodel/config"
	"basemodel/database"
	"basemodel/errs"
	"basemodel/logs"
	"basemodel/tools/jwt"
	"basemodel/tools/randoms"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type service struct {
	repo  repository
	cache *cache.RedisCache
}

func (s *service) register(req RegisterReq) (*RegisterResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	u, err := s.repo.findByUsername(ctx, req.Username)
	if err != nil {
		logs.Errorf("register findByUsername error: %v", err)
		return nil, errs.DBError
	}
	if u != nil {
		return nil, biz.ErrUserNameExisted
	}

	u, err = s.repo.findByEmail(ctx, req.Email)
	if err != nil {
		logs.Errorf("register findByEmail error: %v", err)
		return nil, errs.DBError
	}
	if u != nil {
		return nil, biz.ErrEmailExisted
	}

	password, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		logs.Errorf("register GenerateFromPassword error: %v", err)
		return nil, biz.ErrPasswordFormat
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, errs.DBError
	}
	token := hex.EncodeToString(tokenBytes)
	userId := uuid.New()

	tokenKey := fmt.Sprintf("verify_token:%s", token)
	err = s.cache.Set(tokenKey, userId.String(), 24*60*60)
	if err != nil {
		logs.Errorf("register Set error: %v", err)
		return nil, errs.DBError
	}

	user := model.User{
		Id:            userId,
		Username:      req.Username,
		Password:      string(password),
		Email:         req.Email,
		EmailVerified: false,
		LastLoginTime: time.Now(),
		CurrentPlan:   model.FreePlan,
		Status:        model.UserStatusPending,
		Avatar:        "default",
	}
	err = s.repo.transaction(ctx, func(tx *gorm.DB) error {
		err := s.repo.saveUser(ctx, tx, &user)
		if err != nil {
			logs.Errorf("register saveUser error: %v", err)
			return err
		}
		err = s.sendVerifyEmail(user.Email, user.Username, token)
		if err != nil {
			logs.Errorf("register sendVerifyEmail error: %v", err)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, errs.DBError
	}
	return &RegisterResp{
		Message: "注册成功，请前往邮箱进行验证",
	}, nil
}

func (s *service) sendVerifyEmail(email string, username string, token string) error {
	emailConfig := config.GetConfig().Email
	addr := fmt.Sprintf("%s:%d", emailConfig.GetHost(), emailConfig.GetPort())
	auth := smtp.PlainAuth("", emailConfig.GetUsername(), emailConfig.GetPassword(), emailConfig.GetHost())
	to := []string{email}
	subject := "请验证您的邮箱地址"
	verifyURL := fmt.Sprintf("%s/api/v1/auth/verify-email?token=%s", emailConfig.GetBaseURL(), token)
	body := fmt.Sprintf("尊敬的 %s，\n\n感谢您注册我们的服务！\n\n请点击以下链接验证您的邮箱地址：\n%s\n\n如果链接无法点击，请复制并粘贴到浏览器地址栏中。\n\n谢谢！\n", username, verifyURL)
	msg := []byte("To: " + email + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"\r\n" +
		body + "\r\n")
	err := smtp.SendMail(addr, auth, emailConfig.GetFrom(), to, msg)
	return err
}

func (s *service) verifyEmail(token string) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	tokenKey := fmt.Sprintf("verify_token:%s", token)
	userIdStr, err := s.cache.Get(tokenKey)
	if err != nil {
		logs.Errorf("verifyEmail Get error: %v", err)
		return nil, biz.ErrTokenInvalid
	}
	defer s.cache.Set(tokenKey, "", 1)

	userId, err := uuid.Parse(userIdStr)
	if err != nil {
		logs.Errorf("verifyEmail Parse error: %v", err)
		return nil, biz.ErrTokenInvalid
	}

	u, err := s.repo.findById(ctx, userId)
	if err != nil {
		logs.Errorf("verifyEmail findById error: %v", err)
		return nil, errs.DBError
	}
	if u == nil {
		return nil, biz.ErrUserNotFound
	}

	if u.EmailVerified {
		//直接返回验证成功
		return nil, nil
	}

	u.EmailVerified = true
	u.Status = model.UserStatusNormal
	err = s.repo.transaction(ctx, func(tx *gorm.DB) error {
		return s.repo.updateUser(ctx, tx, u)
	})
	if err != nil {
		logs.Errorf("verifyEmail updateUser error: %v", err)
		return nil, errs.DBError
	}
	return nil, nil
}

func (s *service) login(req LoginReq) (*LoginResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	u, err := s.repo.findByUsernameOrEmail(ctx, req.Username)
	if err != nil {
		logs.Errorf("login findByUsernameOrEmail error: %v", err)
		return nil, errs.DBError
	}
	if u == nil {
		return nil, biz.ErrUserNotFound
	}
	if !u.EmailVerified {
		return nil, biz.ErrEmailNotVerified
	}

	password, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		logs.Errorf("login GenerateFromPassword error: %v", err)
		return nil, errs.DBError
	}
	logs.Infof("password: %s", password)
	err = bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.Password))
	if err != nil {
		return nil, biz.ErrPasswordInvalid
	}
	return s.token(u)
}

func (s *service) token(u *model.User) (*LoginResp, error) {
	expire := config.GetConfig().Jwt.GetExpire()
	refreshExpire := config.GetConfig().Jwt.GetRefresh()
	token, err := jwt.GenToken(u.Id.String(), u.Username, expire)
	if err != nil {
		logs.Errorf("token GenToken error: %v", err)
		return nil, biz.ErrTokenGen
	}
	refreshToken, err := jwt.GenToken(u.Id.String(), u.Username, refreshExpire)
	if err != nil {
		logs.Errorf("token GenToken error: %v", err)
		return nil, biz.ErrTokenGen
	}
	return &LoginResp{
		Expire:        time.Now().Add(expire).UnixMilli(),
		Token:         token,
		RefreshExpire: time.Now().Add(refreshExpire).UnixMilli(),
		RefreshToken:  refreshToken,
		UserInfo: &model.UserDTO{
			Id:          u.Id,
			Username:    u.Username,
			Avatar:      u.Avatar,
			Status:      u.Status,
			CurrentPlan: u.CurrentPlan,
		},
	}, nil
}

func (s *service) refreshToken(refreshToken string) (*LoginResp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	claims, err := jwt.ParseToken(refreshToken)
	if err != nil {
		return nil, biz.ErrTokenInvalid
	}
	userIdStr := claims.UserId
	userId, err := uuid.Parse(userIdStr)
	if err != nil {
		return nil, biz.ErrTokenInvalid
	}

	u, err := s.repo.findById(ctx, userId)
	if err != nil {
		logs.Errorf("refreshToken findById error: %v", err)
		return nil, errs.DBError
	}
	return s.token(u)
}

func (s *service) forgotPassword(forgetReq ForgetPasswordReq) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	u, err := s.repo.findByEmail(ctx, forgetReq.Email)
	if err != nil {
		logs.Errorf("forgetPassword findByEmail error: %v", err)
		return nil, errs.DBError
	}
	if u == nil {
		return nil, biz.ErrUserNotFound
	}

	code, err := randoms.GenCode()
	if err != nil {
		logs.Errorf("forgetPassword Gen6Code error: %v", err)
		return nil, biz.ErrCodeGen
	}

	codeKey := fmt.Sprintf("forget_password_code:%s", u.Email)
	err = s.cache.Set(codeKey, code, 5*60)
	if err != nil {
		logs.Errorf("forgetPassword Set error: %v", err)
		return nil, errs.DBError
	}

	err = s.sendForgetPasswordEmail(u.Email, u.Username, code)
	if err != nil {
		logs.Errorf("forgetPassword sendForgetPasswordEmail error: %v", err)
		return nil, errs.DBError
	}
	return map[string]any{
		"message": "已发送验证码，请查收邮件",
	}, nil
}

func (s *service) sendForgetPasswordEmail(email string, username string, code string) error {
	emailConfig := config.GetConfig().Email
	addr := fmt.Sprintf("%s:%d", emailConfig.GetHost(), emailConfig.GetPort())
	auth := smtp.PlainAuth("", emailConfig.GetUsername(), emailConfig.GetPassword(), emailConfig.GetHost())
	to := []string{email}
	subject := "您的验证码"
	body := fmt.Sprintf("尊敬的 %s，\n\n您正在重置密码，验证码是：%s\n\n验证码5分钟内有效，如非本人操作请忽略。\n\n谢谢！\n", username, code)
	msg := []byte("To: " + email + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"\r\n" +
		body + "\r\n")
	err := smtp.SendMail(addr, auth, emailConfig.GetFrom(), to, msg)
	return err
}

func (s *service) verifyCode(req VerifyCodeReq) (*VerifyCodeResp, error) {
	codeKey := fmt.Sprintf("forget_password_code:%s", req.Email)
	code, err := s.cache.Get(codeKey)
	if err != nil {
		logs.Errorf("verifyCode Get error: %v", err)
		return nil, biz.ErrCodeInvalid
	}

	if code != req.Code {
		return nil, biz.ErrCodeInvalid
	}

	token, err := s.generateResetPasswordToken(req.Email)
	if err != nil {
		logs.Errorf("verifyCode generrateResetPasswordToken error: %v", err)
		return nil, biz.ErrTokenGen
	}
	defer s.cache.Set(codeKey, "", 1)
	return &VerifyCodeResp{
		Message: "验证成功",
		Token:   token,
	}, nil
}

func (s *service) generateResetPasswordToken(email string) (string, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", errs.DBError
	}
	token := hex.EncodeToString(tokenBytes)

	key := fmt.Sprintf("reset_password_token:%s", token)

	err := s.cache.Set(key, email, 15*60)
	if err != nil {
		logs.Errorf("generateResetPasswordToken Set error: %v", err)
		return "", errs.DBError
	}
	return token, nil
}

func (s *service) resetPassword(c context.Context, resetReq ResetPasswordReq) (any, error) {
	tokenKey := fmt.Sprintf("reset_password_token:%s", resetReq.Token)
	email, err := s.cache.Get(tokenKey)
	if err != nil {
		return nil, biz.ErrTokenInvalid
	}
	defer s.cache.Set(tokenKey, "", 1)
	ctx, cancel := context.WithTimeout(c, time.Second*5)
	defer cancel()

	if email != resetReq.Email {
		return nil, biz.ErrEmailNotMatch
	}

	u, err := s.repo.findByEmail(ctx, resetReq.Email)
	if err != nil {
		logs.Errorf("resetPassword findByEmail error: %v", err)
		return nil, errs.DBError
	}
	if u == nil {
		return nil, biz.ErrUserNotFound
	}

	newPassword, err := bcrypt.GenerateFromPassword([]byte(resetReq.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		logs.Errorf("resetPassword GenerateFromPassword error: %v", err)
		return nil, biz.ErrPasswordFormat
	}

	u.Password = string(newPassword)
	err = s.repo.transaction(ctx, func(tx *gorm.DB) error {
		return s.repo.updateUser(ctx, tx, u)
	})
	if err != nil {
		logs.Errorf("resetPassword updateUser error: %v", err)
		return nil, errs.DBError
	}
	return map[string]any{
		"message": "密码重置成功",
	}, nil
}

func newService() *service {
	return &service{
		repo:  newModel(database.GetPostgresDB().GormDB),
		cache: cache.NewRedisCache(),
	}
}
