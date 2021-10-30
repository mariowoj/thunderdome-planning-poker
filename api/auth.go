package api

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/StevenWeathers/thunderdome-planning-poker/model"
	ldap "github.com/go-ldap/ldap/v3"
	"github.com/spf13/viper"
	"gopkg.in/go-playground/validator.v9"
)

type UserAccount struct {
	Name      string `json:"name" validate:"required"`
	Email     string `json:"email" validate:"required,email"`
	Password1 string `json:"password1" validate:"required,min=6,max=72"`
	Password2 string `json:"password2" validate:"required,min=6,max=72,eqfield=Password1"`
}

type UserPassword struct {
	Password1 string `json:"password1" validate:"required,min=6,max=72"`
	Password2 string `json:"password2" validate:"required,min=6,max=72,eqfield=Password1"`
}

// handleLogin attempts to login the user by comparing email/password to whats in DB
// @Summary Login
// @Description attempts to log the user in with provided credentials
// @Description *Endpoint only available when LDAP is not enabled
// @Tags auth
// @Produce  json
// @Success 200 object standardJsonResponse{data=model.User}
// @Failure 401 object standardJsonResponse{}
// @Failure 500 object standardJsonResponse{}
// @Router /auth [post]
func (a *api) handleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyVal := a.getJSONRequestBody(r, w)
		UserEmail := strings.ToLower(keyVal["warriorEmail"].(string))
		UserPassword := keyVal["warriorPassword"].(string)

		authedUser, err := a.authUserDatabase(UserEmail, UserPassword)
		if err != nil {
			a.respondWithStandardJSON(w, http.StatusUnauthorized, false, nil, nil, nil)
			return
		}

		cookie := a.createCookie(authedUser.UserID)
		if cookie != nil {
			http.SetCookie(w, cookie)
		} else {
			a.respondWithStandardJSON(w, http.StatusInternalServerError, false, nil, nil, nil)
			return
		}

		a.respondWithStandardJSON(w, http.StatusOK, true, nil, authedUser, nil)
	}
}

// handleLdapLogin attempts to authenticate the user by looking up and authenticating
// via ldap, and then creates the user if not existing and logs them in
// @Summary Login LDAP
// @Description attempts to log the user in with provided credentials
// @Description *Endpoint only available when LDAP is enabled
// @Tags auth
// @Produce  json
// @Success 200 object standardJsonResponse{data=model.User}
// @Failure 401 object standardJsonResponse{}
// @Failure 500 object standardJsonResponse{}
// @Router /auth/ldap [post]
func (a *api) handleLdapLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyVal := a.getJSONRequestBody(r, w)
		UserEmail := strings.ToLower(keyVal["warriorEmail"].(string))
		UserPassword := keyVal["warriorPassword"].(string)

		authedUser, err := a.authAndCreateUserLdap(UserEmail, UserPassword)
		if err != nil {
			a.respondWithStandardJSON(w, http.StatusUnauthorized, false, nil, nil, nil)
			return
		}

		cookie := a.createCookie(authedUser.UserID)
		if cookie != nil {
			http.SetCookie(w, cookie)
		} else {
			a.respondWithStandardJSON(w, http.StatusInternalServerError, false, nil, nil, nil)
			return
		}
		a.respondWithStandardJSON(w, http.StatusOK, true, nil, authedUser, nil)
	}
}

// handleLogout clears the user cookie(s) ending session
// @Summary Logout
// @Description Logs the user out by deleting session cookies
// @Tags auth
// @Success 200
// @Router /auth/logout [delete]
func (a *api) handleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.clearUserCookies(w)
		return
	}
}

// handleCreateGuestUser registers a user as a guest user
// @Summary Create Guest User
// @Description Registers a user as a guest (non authenticated)
// @Tags auth
// @Success 200 object standardJsonResponse{data=model.User}
// @Failure 400 object standardJsonResponse{}
// @Failure 500 object standardJsonResponse{}
// @Router /auth/guest [post]
func (a *api) handleCreateGuestUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		AllowGuests := viper.GetBool("config.allow_guests")
		if !AllowGuests {
			a.respondWithStandardJSON(w, http.StatusBadRequest, false, nil, nil, nil)
			return
		}

		keyVal := a.getJSONRequestBody(r, w)

		UserName := keyVal["warriorName"].(string)

		newUser, err := a.db.CreateUserGuest(UserName)
		if err != nil {
			errors := make([]string, 0)
			errors = append(errors, err.Error())
			a.respondWithStandardJSON(w, http.StatusInternalServerError, false, errors, nil, nil)
			return
		}

		a.createUserCookie(w, false, newUser.UserID)

		a.respondWithStandardJSON(w, http.StatusOK, true, nil, newUser, nil)
	}
}

// handleUserRegistration registers a new authenticated user
// @Summary Create User
// @Description Registers a user (authenticated)
// @Tags auth
// @Success 200 object standardJsonResponse{data=model.User}
// @Failure 400 object standardJsonResponse{}
// @Failure 500 object standardJsonResponse{}
// @Router /auth/register [post]
func (a *api) handleUserRegistration() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		AllowRegistration := viper.GetBool("config.allow_registration")
		if !AllowRegistration {
			a.respondWithStandardJSON(w, http.StatusBadRequest, false, nil, nil, nil)
		}

		keyVal := a.getJSONRequestBody(r, w)

		ActiveUserID, _ := a.validateUserCookie(w, r)

		UserName, UserEmail, UserPassword, accountErr := ValidateUserAccount(
			keyVal["warriorName"].(string),
			strings.ToLower(keyVal["warriorEmail"].(string)),
			keyVal["warriorPassword1"].(string),
			keyVal["warriorPassword2"].(string),
		)

		if accountErr != nil {
			errors := make([]string, 0)
			errors = append(errors, accountErr.Error())
			a.respondWithStandardJSON(w, http.StatusBadRequest, false, errors, nil, nil)
			return
		}

		newUser, VerifyID, err := a.db.CreateUserRegistered(UserName, UserEmail, UserPassword, ActiveUserID)
		if err != nil {
			errors := make([]string, 0)
			errors = append(errors, err.Error())
			a.respondWithStandardJSON(w, http.StatusInternalServerError, false, errors, nil, nil)
			return
		}

		a.createUserCookie(w, true, newUser.UserID)

		a.email.SendWelcome(UserName, UserEmail, VerifyID)

		a.respondWithStandardJSON(w, http.StatusOK, true, nil, newUser, nil)
	}
}

// handleForgotPassword attempts to send a password reset email
// @Summary Forgot Password
// @Description Sends a forgot password reset email to user
// @Tags auth
// @Success 200 object standardJsonResponse{}
// @Router /auth/forgot-password [post]
func (a *api) handleForgotPassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyVal := a.getJSONRequestBody(r, w)
		UserEmail := strings.ToLower(keyVal["warriorEmail"].(string))

		ResetID, UserName, resetErr := a.db.UserResetRequest(UserEmail)
		if resetErr == nil {
			a.email.SendForgotPassword(UserName, UserEmail, ResetID)
			return
		}

		a.respondWithStandardJSON(w, http.StatusOK, true, nil, nil, nil)
	}
}

// handleResetPassword attempts to reset a users password
// @Summary Reset Password
// @Description Resets the users password
// @Tags auth
// @Success 200 object standardJsonResponse{}
// @Success 400 object standardJsonResponse{}
// @Success 500 object standardJsonResponse{}
// @Router /auth/reset-password [patch]
func (a *api) handleResetPassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyVal := a.getJSONRequestBody(r, w)
		ResetID := keyVal["resetId"].(string)

		UserPassword, passwordErr := ValidateUserPassword(
			keyVal["warriorPassword1"].(string),
			keyVal["warriorPassword2"].(string),
		)

		if passwordErr != nil {
			a.respondWithStandardJSON(w, http.StatusBadRequest, false, nil, nil, nil)
			return
		}

		UserName, UserEmail, resetErr := a.db.UserResetPassword(ResetID, UserPassword)
		if resetErr != nil {
			log.Println("error attempting to reset user password : " + resetErr.Error() + "\n")
			errors := make([]string, 0)
			errors = append(errors, resetErr.Error())
			a.respondWithStandardJSON(w, http.StatusInternalServerError, false, errors, nil, nil)
			return
		}

		a.email.SendPasswordReset(UserName, UserEmail)

		a.respondWithStandardJSON(w, http.StatusOK, true, nil, nil, nil)
	}
}

// handleUpdatePassword attempts to update a users password
// @Summary Update Password
// @Description Updates the users password
// @Tags auth
// @Success 200 object standardJsonResponse{}
// @Success 400 object standardJsonResponse{}
// @Success 500 object standardJsonResponse{}
// @Router /auth/update-password [patch]
func (a *api) handleUpdatePassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyVal := a.getJSONRequestBody(r, w)
		UserID := r.Context().Value(contextKeyUserID).(string)

		UserPassword, passwordErr := ValidateUserPassword(
			keyVal["warriorPassword1"].(string),
			keyVal["warriorPassword2"].(string),
		)

		if passwordErr != nil {
			a.respondWithStandardJSON(w, http.StatusBadRequest, false, nil, nil, nil)
			return
		}

		UserName, UserEmail, updateErr := a.db.UserUpdatePassword(UserID, UserPassword)
		if updateErr != nil {
			log.Println("error attempting to update user password : " + updateErr.Error() + "\n")
			errors := make([]string, 0)
			errors = append(errors, updateErr.Error())
			a.respondWithStandardJSON(w, http.StatusInternalServerError, false, errors, nil, nil)
			return
		}

		a.email.SendPasswordUpdate(UserName, UserEmail)

		a.respondWithStandardJSON(w, http.StatusOK, true, nil, nil, nil)
	}
}

// handleAccountVerification attempts to verify a users account
// @Summary Verify User
// @Description Updates the users verified email status
// @Tags auth
// @Success 200 object standardJsonResponse{}
// @Success 500 object standardJsonResponse{}
// @Router /auth/verify [patch]
func (a *api) handleAccountVerification() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyVal := a.getJSONRequestBody(r, w)
		VerifyID := keyVal["verifyId"].(string)

		verifyErr := a.db.VerifyUserAccount(VerifyID)
		if verifyErr != nil {
			log.Println("error attempting to verify user account : " + verifyErr.Error() + "\n")
			errors := make([]string, 0)
			errors = append(errors, verifyErr.Error())
			a.respondWithStandardJSON(w, http.StatusInternalServerError, false, errors, nil, nil)
			return
		}

		a.respondWithStandardJSON(w, http.StatusOK, true, nil, nil, nil)
	}
}

/*
	UTILS (
		- ldap auth should get moved out of api package
		- cookie should get moved out of the api package
*/

// ValidateUserAccount makes sure user name, email, and password are valid before creating the account
func ValidateUserAccount(name string, email string, pwd1 string, pwd2 string) (UserName string, UserEmail string, UpdatedPassword string, validateErr error) {
	v := validator.New()
	a := UserAccount{
		Name:      name,
		Email:     email,
		Password1: pwd1,
		Password2: pwd2,
	}
	err := v.Struct(a)

	return name, email, pwd1, err
}

// ValidateUserPassword makes sure user password is valid before updating the password
func ValidateUserPassword(pwd1 string, pwd2 string) (UpdatedPassword string, validateErr error) {
	v := validator.New()
	a := UserPassword{
		Password1: pwd1,
		Password2: pwd2,
	}
	err := v.Struct(a)

	return pwd1, err
}

// createUserCookie creates the users cookie
func (a *api) createUserCookie(w http.ResponseWriter, isRegistered bool, UserID string) {
	var cookiedays = 365 // 356 days
	if isRegistered {
		cookiedays = 30 // 30 days
	}

	encoded, err := a.cookie.Encode(a.config.SecureCookieName, UserID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return

	}

	cookie := &http.Cookie{
		Name:     a.config.SecureCookieName,
		Value:    encoded,
		Path:     a.config.PathPrefix + "/",
		HttpOnly: true,
		Domain:   a.config.AppDomain,
		MaxAge:   86400 * cookiedays,
		Secure:   a.config.SecureCookieFlag,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
}

// clearUserCookies wipes the frontend and backend cookies
// used in the event of bad cookie reads
func (a *api) clearUserCookies(w http.ResponseWriter) {
	feCookie := &http.Cookie{
		Name:   a.config.FrontendCookieName,
		Value:  "",
		Path:   a.config.PathPrefix + "/",
		MaxAge: -1,
	}
	beCookie := &http.Cookie{
		Name:     a.config.SecureCookieName,
		Value:    "",
		Path:     a.config.PathPrefix + "/",
		Domain:   a.config.AppDomain,
		Secure:   a.config.SecureCookieFlag,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		HttpOnly: true,
	}

	http.SetCookie(w, feCookie)
	http.SetCookie(w, beCookie)
}

// validateUserCookie returns the UserID from secure cookies or errors if failures getting it
func (a *api) validateUserCookie(w http.ResponseWriter, r *http.Request) (string, error) {
	var UserID string

	if cookie, err := r.Cookie(a.config.SecureCookieName); err == nil {
		var value string
		if err = a.cookie.Decode(a.config.SecureCookieName, cookie.Value, &value); err == nil {
			UserID = value
		} else {
			log.Println("error in reading user cookie : " + err.Error() + "\n")
			a.clearUserCookies(w)
			return "", errors.New("invalid user cookies")
		}
	} else {
		log.Println("error in reading user cookie : " + err.Error() + "\n")
		a.clearUserCookies(w)
		return "", errors.New("invalid user cookies")
	}

	return UserID, nil
}

func (a *api) createCookie(UserID string) *http.Cookie {
	encoded, err := a.cookie.Encode(a.config.SecureCookieName, UserID)
	var NewCookie *http.Cookie

	if err == nil {
		NewCookie = &http.Cookie{
			Name:     a.config.SecureCookieName,
			Value:    encoded,
			Path:     a.config.PathPrefix + "/",
			HttpOnly: true,
			Domain:   a.config.AppDomain,
			MaxAge:   86400 * 30, // 30 days
			Secure:   a.config.SecureCookieFlag,
			SameSite: http.SameSiteStrictMode,
		}
	}
	return NewCookie
}

// @todo - update database function to handle these errors instead
func (a *api) authUserDatabase(UserEmail string, UserPassword string) (*model.User, error) {
	AuthedUser, err := a.db.AuthUser(UserEmail, UserPassword)
	if err != nil {
		log.Println("Failed authenticating user", UserEmail)
	} else if AuthedUser == nil {
		log.Println("Unknown user", UserEmail)
	}
	return AuthedUser, err
}

// Authenticate using LDAP and if user does not exist, automatically add user as a verified user
func (a *api) authAndCreateUserLdap(UserName string, UserPassword string) (*model.User, error) {
	var AuthedUser *model.User
	l, err := ldap.DialURL(viper.GetString("auth.ldap.url"))
	if err != nil {
		log.Println("Failed connecting to ldap server at", viper.GetString("auth.ldap.url"))
		return AuthedUser, err
	}
	defer l.Close()
	if viper.GetBool("auth.ldap.use_tls") {
		err = l.StartTLS(&tls.Config{InsecureSkipVerify: true})
		if err != nil {
			log.Println("Failed securing ldap connection", err)
			return AuthedUser, err
		}
	}

	if viper.GetString("auth.ldap.bindname") != "" {
		err = l.Bind(viper.GetString("auth.ldap.bindname"), viper.GetString("auth.ldap.bindpass"))
		if err != nil {
			log.Println("Failed binding for authentication:", err)
			return AuthedUser, err
		}
	}

	searchRequest := ldap.NewSearchRequest(viper.GetString("auth.ldap.basedn"),
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf(viper.GetString("auth.ldap.filter"), ldap.EscapeFilter(UserName)),
		[]string{"dn", viper.GetString("auth.ldap.mail_attr"), viper.GetString("auth.ldap.cn_attr")},
		nil,
	)

	sr, err := l.Search(searchRequest)
	if err != nil {
		log.Println("Failed performing ldap search query for", UserName, ":", err)
		return AuthedUser, err
	}

	if len(sr.Entries) != 1 {
		log.Println("User", UserName, "does not exist or too many entries returned")
		return AuthedUser, errors.New("user not found")
	}

	userdn := sr.Entries[0].DN
	useremail := sr.Entries[0].GetAttributeValue(viper.GetString("auth.ldap.mail_attr"))
	usercn := sr.Entries[0].GetAttributeValue(viper.GetString("auth.ldap.cn_attr"))

	err = l.Bind(userdn, UserPassword)
	if err != nil {
		log.Println("Failed authenticating user ", UserName)
		return AuthedUser, err
	}

	AuthedUser, err = a.db.GetUserByEmail(useremail)
	if AuthedUser == nil {
		log.Println("User", useremail, "does not exist in database, auto-recruit")
		newUser, verifyID, err := a.db.CreateUserRegistered(usercn, useremail, "", "")
		if err != nil {
			log.Println("Failed auto-creating new user", err)
			return AuthedUser, err
		}
		err = a.db.VerifyUserAccount(verifyID)
		if err != nil {
			log.Println("Failed verifying new user", err)
			return AuthedUser, err
		}
		AuthedUser = newUser
	}

	return AuthedUser, nil
}
