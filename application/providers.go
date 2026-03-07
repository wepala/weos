package application

// This file contains Fx provider functions for application services.
// Each provider function uses fx.In struct injection to declare dependencies.
//
// Example provider pattern:
//
//	func ProvideUserService(params struct {
//		fx.In
//		Config     config.Config
//		UserRepo   repositories.UserRepository
//		Logger     entities.Logger
//		EventStore domain.EventStore
//		EventDispatcher *domain.EventDispatcher
//	}) UserServiceInterface {
//		return NewUserService(
//			params.UserRepo,
//			params.Logger,
//			params.EventStore,
//			params.EventDispatcher,
//		)
//	}
//
// For named dependencies (e.g., multiple agents of the same type),
// use fx.ResultTags in module.go and named struct tags in providers:
//
//	// In module.go:
//	fx.Provide(fx.Annotate(ProvideMyAgent, fx.ResultTags(`name:"myAgent"`)))
//
//	// In provider:
//	func ProvideMyService(params struct {
//		fx.In
//		Agent agent.Agent `name:"myAgent"`
//	}) MyServiceInterface { ... }
