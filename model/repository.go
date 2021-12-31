//go:generate moq -out mocks_test.go -pkg model_test . EndToEndProjection
package model

type EndToEndProjection interface {
	Projection
}
