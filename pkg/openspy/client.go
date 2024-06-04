package openspy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	BaseURL = "http://account.openspy.net/api/"
)

type RequestError struct {
	RequestURL *url.URL
	StatusCode int
}

func newRequestError(requestURL *url.URL, statusCode int) *RequestError {
	return &RequestError{
		RequestURL: requestURL,
		StatusCode: statusCode,
	}
}

func (e RequestError) Error() string {
	return fmt.Sprintf("request to %s failed with status code %d", e.RequestURL.Redacted(), e.StatusCode)
}

type APIError struct {
	Code    string
	Message string
}

func newAPIError(code, message string) *APIError {
	return &APIError{
		Code:    code,
		Message: message,
	}
}

func (e APIError) Error() string {
	return fmt.Sprintf("%s (%s)", e.Message, e.Code)
}

type Client struct {
	client  http.Client
	baseURL string

	authToken string
}

func New(baseURL string, timeout int) *Client {
	return &Client{
		client: http.Client{
			Timeout: time.Duration(timeout) * time.Second,
		},
		baseURL: baseURL,
	}
}

func (c *Client) CreateAccount(email, password string, partnerCode int) error {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return err
	}

	u = u.JoinPath("auth", "register")

	payload, err := json.Marshal(map[string]any{
		"email":       email,
		"password":    password,
		"partnercode": partnerCode,
	})
	if err != nil {
		return err
	}

	req, err := c.createRequest(http.MethodPut, u.String(), bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	body, err := c.do(req)
	if err != nil {
		return err
	}

	var res authenticationResponse
	err = json.Unmarshal(body, &res)
	if err != nil {
		return err
	}

	// Store auth token
	c.authToken = res.AuthToken

	return nil
}

func (c *Client) CreateProfile(nick string, namespaceID int) error {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return err
	}

	u = u.JoinPath("profile")

	payload, err := json.Marshal(map[string]any{
		"profile": map[string]any{
			"nick":        nick,
			"uniquenick":  nick,
			"namespaceid": strconv.Itoa(namespaceID),
		},
	})
	if err != nil {
		return err
	}

	req, err := c.createRequest(http.MethodPut, u.String(), bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	err = c.authenticateRequest(req)
	if err != nil {
		return err
	}

	body, err := c.do(req)
	if err != nil {
		return err
	}

	var response putProfileResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) GetProfiles() ([]ProfileDTO, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}

	u = u.JoinPath("profile")

	req, err := c.createRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	err = c.authenticateRequest(req)
	if err != nil {
		return nil, err
	}

	body, err := c.do(req)
	if err != nil {
		return nil, err
	}

	var profiles []ProfileDTO
	err = json.Unmarshal(body, &profiles)
	if err != nil {
		return nil, err
	}

	return profiles, nil
}

func (c *Client) createRequest(method string, u string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

func (c *Client) authenticateRequest(req *http.Request) error {
	if c.authToken == "" {
		return fmt.Errorf("no authentication details configured")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.authToken))
	return nil
}

func (c *Client) do(req *http.Request) ([]byte, error) {
	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, newRequestError(req.URL, res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	err = res.Body.Close()
	if err != nil {
		return nil, err
	}

	// Cannot be an error response if not an object, skip error check and return
	if !bytes.HasPrefix(body, []byte("{")) || !bytes.HasSuffix(body, []byte("}")) {
		return body, nil
	}

	var e errorResponse
	err = json.Unmarshal(body, &e)
	if err != nil {
		return nil, err
	}

	if e.Error != nil {
		return nil, newAPIError(e.Error.Code, e.Error.Message)
	}

	return body, nil
}
