package openspy

type errorDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error *errorDTO `json:"error"`
}

type authenticationResponse struct {
	AuthToken string `json:"auth_token"`
}

type ProfileDTO struct {
	ID          int    `json:"id"`
	Nick        string `json:"nick"`
	UniqueNick  string `json:"uniquenick"`
	NamespaceID int    `json:"namespaceid"`
}

type putProfileResponse struct {
	Profile ProfileDTO `json:"profile"`
}
