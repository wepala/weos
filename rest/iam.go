package rest

import "golang.org/x/net/context"

type IAMConnection struct {
	ID          string `json:"id"`
	UserID      string `json:"userId"`
	ProviderID  string `json:"providerID"`
	ResourceID  string `json:"resourceId"`
	AccountID   string `json:"accountId"`
	ClientID    string `json:"clientId"`
	AccessToken string `json:"access_token"`
}

type IAMService interface {
	GetConnectionsByResource(ctxt context.Context, logger Log, accountID string, userID string, provider *[]string) (connections []IAMConnection, err error)
}
