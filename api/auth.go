package api

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/spf13/viper"
)

type userLoginRequestBody struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleLogin attempts to log in the user
// @Summary Login
// @Description attempts to log the user in with provided credentials
// @Description *Endpoint only available when LDAP is not enabled
// @Tags auth
// @Produce  json
// @Param credentials body userLoginRequestBody false "user login object"
// @Success 200 object standardJsonResponse{data=model.User}
// @Failure 401 object standardJsonResponse{}
// @Failure 500 object standardJsonResponse{}
// @Router /auth [post]
func (a *api) handleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, bodyErr := ioutil.ReadAll(r.Body)
		if bodyErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, bodyErr.Error()))
			return
		}

		var u = userLoginRequestBody{}
		jsonErr := json.Unmarshal(body, &u)
		if jsonErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, jsonErr.Error()))
			return
		}

		authedUser, sessionId, err := a.db.AuthUser(strings.ToLower(u.Email), u.Password)
		if err != nil {
			a.Failure(w, r, http.StatusUnauthorized, Errorf(EINVALID, "INVALID_LOGIN"))
			return
		}

		cookieErr := a.createSessionCookie(w, sessionId)
		if cookieErr != nil {
			a.Failure(w, r, http.StatusInternalServerError, Errorf(EINVALID, "INVALID_COOKIE"))
			return
		}

		a.Success(w, r, http.StatusOK, authedUser, nil)
	}
}

// handleLdapLogin attempts to authenticate the user by looking up and authenticating
// via ldap, and then creates the user if not existing and logs them in
// @Summary Login LDAP
// @Description attempts to log the user in with provided credentials
// @Description *Endpoint only available when LDAP is enabled
// @Tags auth
// @Produce json
// @Param credentials body userLoginRequestBody false "user login object"
// @Success 200 object standardJsonResponse{data=model.User}
// @Failure 401 object standardJsonResponse{}
// @Failure 500 object standardJsonResponse{}
// @Router /auth/ldap [post]
func (a *api) handleLdapLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, bodyErr := ioutil.ReadAll(r.Body)
		if bodyErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, bodyErr.Error()))
			return
		}

		var u = userLoginRequestBody{}
		jsonErr := json.Unmarshal(body, &u)
		if jsonErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, jsonErr.Error()))
			return
		}

		authedUser, sessionId, err := a.authAndCreateUserLdap(strings.ToLower(u.Email), u.Password)
		if err != nil {
			a.Failure(w, r, http.StatusUnauthorized, Errorf(EINVALID, "INVALID_LOGIN"))
			return
		}

		cookieErr := a.createSessionCookie(w, sessionId)
		if cookieErr != nil {
			a.Failure(w, r, http.StatusInternalServerError, Errorf(EINVALID, "INVALID_COOKIE"))
			return
		}

		a.Success(w, r, http.StatusOK, authedUser, nil)
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
		SessionId, cookieErr := a.validateSessionCookie(w, r)
		if cookieErr != nil {
			a.Failure(w, r, http.StatusUnauthorized, Errorf(EINVALID, "INVALID_USER"))
			return
		}

		err := a.db.DeleteSession(SessionId)
		if err != nil {
			a.Failure(w, r, http.StatusInternalServerError, err)
			return
		}

		a.clearUserCookies(w)
		a.Success(w, r, http.StatusOK, nil, nil)
	}
}

type guestUserCreateRequestBody struct {
	Name string `json:"name"`
}

// handleCreateGuestUser registers a user as a guest user
// @Summary Create Guest User
// @Description Registers a user as a guest (non-authenticated)
// @Tags auth
// @Produce json
// @Param user body guestUserCreateRequestBody false "guest user object"
// @Success 200 object standardJsonResponse{data=model.User}
// @Failure 400 object standardJsonResponse{}
// @Failure 500 object standardJsonResponse{}
// @Router /auth/guest [post]
func (a *api) handleCreateGuestUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		AllowGuests := viper.GetBool("config.allow_guests")
		if !AllowGuests {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, "GUESTS_USERS_DISABLED"))
			return
		}

		body, bodyErr := ioutil.ReadAll(r.Body)
		if bodyErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, bodyErr.Error()))
			return
		}

		var u = guestUserCreateRequestBody{}
		jsonErr := json.Unmarshal(body, &u)
		if jsonErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, jsonErr.Error()))
			return
		}

		newUser, err := a.db.CreateUserGuest(u.Name)
		if err != nil {
			a.Failure(w, r, http.StatusInternalServerError, err)
			return
		}

		cookieErr := a.createUserCookie(w, newUser.Id)
		if cookieErr != nil {
			a.Failure(w, r, http.StatusInternalServerError, Errorf(EINVALID, "INVALID_COOKIE"))
			return
		}

		a.Success(w, r, http.StatusOK, newUser, nil)
	}
}

type userRegisterRequestBody struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Password1 string `json:"password1"`
	Password2 string `json:"password2"`
}

// handleUserRegistration registers a new authenticated user
// @Summary Create User
// @Description Registers a user (authenticated)
// @Tags auth
// @Produce json
// @Param user body userRegisterRequestBody false "new user object"
// @Success 200 object standardJsonResponse{data=model.User}
// @Failure 400 object standardJsonResponse{}
// @Failure 500 object standardJsonResponse{}
// @Router /auth/register [post]
func (a *api) handleUserRegistration() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		AllowRegistration := viper.GetBool("config.allow_registration")
		if !AllowRegistration {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, "USER_REGISTRATION_DISABLED"))
		}

		body, bodyErr := ioutil.ReadAll(r.Body)
		if bodyErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, bodyErr.Error()))
			return
		}

		var u = userRegisterRequestBody{}
		jsonErr := json.Unmarshal(body, &u)
		if jsonErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, jsonErr.Error()))
			return
		}

		ActiveUserID, _ := a.validateUserCookie(w, r)

		UserName, UserEmail, UserPassword, accountErr := validateUserAccountWithPasswords(
			u.Name,
			strings.ToLower(u.Email),
			u.Password1,
			u.Password2,
		)

		if accountErr != nil {
			a.Failure(w, r, http.StatusBadRequest, accountErr)
			return
		}

		newUser, VerifyID, SessionID, err := a.db.CreateUserRegistered(UserName, UserEmail, UserPassword, ActiveUserID)
		if err != nil {
			a.Failure(w, r, http.StatusInternalServerError, err)
			return
		}

		a.email.SendWelcome(UserName, UserEmail, VerifyID)

		if ActiveUserID != "" {
			a.clearUserCookies(w)
		}

		cookieErr := a.createSessionCookie(w, SessionID)
		if cookieErr != nil {
			a.Failure(w, r, http.StatusInternalServerError, Errorf(EINVALID, "INVALID_COOKIE"))
			return
		}

		a.Success(w, r, http.StatusOK, newUser, nil)
	}
}

type forgotPasswordRequestBody struct {
	Email string `json:"email"`
}

// handleForgotPassword attempts to send a password reset email
// @Summary Forgot Password
// @Description Sends a forgot password reset email to user
// @Tags auth
// @Produce json
// @Param user body forgotPasswordRequestBody false "forgot password object"
// @Success 200 object standardJsonResponse{}
// @Router /auth/forgot-password [post]
func (a *api) handleForgotPassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, bodyErr := ioutil.ReadAll(r.Body)
		if bodyErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, bodyErr.Error()))
			return
		}

		var u = forgotPasswordRequestBody{}
		jsonErr := json.Unmarshal(body, &u)
		if jsonErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, jsonErr.Error()))
			return
		}

		UserEmail := strings.ToLower(u.Email)

		ResetID, UserName, resetErr := a.db.UserResetRequest(UserEmail)
		if resetErr == nil {
			a.email.SendForgotPassword(UserName, UserEmail, ResetID)
		}

		a.Success(w, r, http.StatusOK, nil, nil)
	}
}

type resetPasswordRequestBody struct {
	ResetID   string `json:"resetId"`
	Password1 string `json:"password1"`
	Password2 string `json:"password2"`
}

// handleResetPassword attempts to reset a user's password
// @Summary Reset Password
// @Description Resets the user's password
// @Tags auth
// @Produce json
// @Param reset body resetPasswordRequestBody false "reset password object"
// @Success 200 object standardJsonResponse{}
// @Success 400 object standardJsonResponse{}
// @Success 500 object standardJsonResponse{}
// @Router /auth/reset-password [patch]
func (a *api) handleResetPassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, bodyErr := ioutil.ReadAll(r.Body)
		if bodyErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, bodyErr.Error()))
			return
		}

		var u = resetPasswordRequestBody{}
		jsonErr := json.Unmarshal(body, &u)
		if jsonErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, jsonErr.Error()))
			return
		}

		UserPassword, passwordErr := validateUserPassword(
			u.Password1,
			u.Password2,
		)

		if passwordErr != nil {
			a.Failure(w, r, http.StatusBadRequest, passwordErr)
			return
		}

		UserName, UserEmail, resetErr := a.db.UserResetPassword(u.ResetID, UserPassword)
		if resetErr != nil {
			a.Failure(w, r, http.StatusInternalServerError, resetErr)
			return
		}

		a.email.SendPasswordReset(UserName, UserEmail)

		a.Success(w, r, http.StatusOK, nil, nil)
	}
}

type updatePasswordRequestBody struct {
	Password1 string `json:"password1"`
	Password2 string `json:"password2"`
}

// handleUpdatePassword attempts to update a user's password
// @Summary Update Password
// @Description Updates the user's password
// @Tags auth
// @Produce json
// @Param passwords body updatePasswordRequestBody false "update password object"
// @Success 200 object standardJsonResponse{}
// @Success 400 object standardJsonResponse{}
// @Success 500 object standardJsonResponse{}
// @Security ApiKeyAuth
// @Router /auth/update-password [patch]
func (a *api) handleUpdatePassword() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		UserID := r.Context().Value(contextKeyUserID).(string)
		body, bodyErr := ioutil.ReadAll(r.Body)
		if bodyErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, bodyErr.Error()))
			return
		}

		var u = updatePasswordRequestBody{}
		jsonErr := json.Unmarshal(body, &u)
		if jsonErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, jsonErr.Error()))
			return
		}

		UserPassword, passwordErr := validateUserPassword(
			u.Password1,
			u.Password2,
		)

		if passwordErr != nil {
			a.Failure(w, r, http.StatusBadRequest, passwordErr)
			return
		}

		UserName, UserEmail, updateErr := a.db.UserUpdatePassword(UserID, UserPassword)
		if updateErr != nil {
			a.Failure(w, r, http.StatusInternalServerError, updateErr)
			return
		}

		a.email.SendPasswordUpdate(UserName, UserEmail)

		a.Success(w, r, http.StatusOK, nil, nil)
	}
}

type verificationRequestBody struct {
	VerifyID string `json:"verifyId"`
}

// handleAccountVerification attempts to verify a users account
// @Summary Verify User
// @Description Updates the users verified email status
// @Tags auth
// @Produce json
// @Param verify body verificationRequestBody false "verify object"
// @Success 200 object standardJsonResponse{}
// @Success 500 object standardJsonResponse{}
// @Router /auth/verify [patch]
func (a *api) handleAccountVerification() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, bodyErr := ioutil.ReadAll(r.Body)
		if bodyErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, bodyErr.Error()))
			return
		}

		var u = verificationRequestBody{}
		jsonErr := json.Unmarshal(body, &u)
		if jsonErr != nil {
			a.Failure(w, r, http.StatusBadRequest, Errorf(EINVALID, jsonErr.Error()))
			return
		}

		verifyErr := a.db.VerifyUserAccount(u.VerifyID)
		if verifyErr != nil {
			a.Failure(w, r, http.StatusInternalServerError, verifyErr)
			return
		}

		a.Success(w, r, http.StatusOK, nil, nil)
	}
}
