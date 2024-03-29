// Code generated by moq; DO NOT EDIT.
// github.com/matryer/moq

package model_test

import (
	"database/sql"
	"github.com/casbin/casbin/v2"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/wepala/weos/model"
	"gorm.io/gorm"
	"net/http"
	"sync"
)

// Ensure, that ContainerMock does implement model.Container.
// If this is not the case, regenerate this file with moq.
var _ model.Container = &ContainerMock{}

// ContainerMock is a mock implementation of model.Container.
//
// 	func TestSomethingThatUsesContainer(t *testing.T) {
//
// 		// make and configure a mocked model.Container
// 		mockedContainer := &ContainerMock{
// 			GetCommandDispatcherFunc: func(name string) (model.CommandDispatcher, error) {
// 				panic("mock out the GetCommandDispatcher method")
// 			},
// 			GetConfigFunc: func() *openapi3.Swagger {
// 				panic("mock out the GetConfig method")
// 			},
// 			GetDBConnectionFunc: func(name string) (*sql.DB, error) {
// 				panic("mock out the GetDBConnection method")
// 			},
// 			GetEntityFactoriesFunc: func() map[string]model.EntityFactory {
// 				panic("mock out the GetEntityFactories method")
// 			},
// 			GetEntityFactoryFunc: func(name string) (model.EntityFactory, error) {
// 				panic("mock out the GetEntityFactory method")
// 			},
// 			GetEntityRepositoryFunc: func(name string) (model.EntityRepository, error) {
// 				panic("mock out the GetEntityRepository method")
// 			},
// 			GetEventStoreFunc: func(name string) (model.EventRepository, error) {
// 				panic("mock out the GetEventStore method")
// 			},
// 			GetGormDBConnectionFunc: func(name string) (*gorm.DB, error) {
// 				panic("mock out the GetGormDBConnection method")
// 			},
// 			GetHTTPClientFunc: func(name string) (*http.Client, error) {
// 				panic("mock out the GetHTTPClient method")
// 			},
// 			GetLogFunc: func(name string) (model.Log, error) {
// 				panic("mock out the GetLog method")
// 			},
// 			GetPermissionEnforcerFunc: func(name string) (*casbin.Enforcer, error) {
// 				panic("mock out the GetPermissionEnforcer method")
// 			},
// 			GetProjectionFunc: func(name string) (model.Projection, error) {
// 				panic("mock out the GetProjection method")
// 			},
// 			RegisterCommandDispatcherFunc: func(name string, dispatcher model.CommandDispatcher)  {
// 				panic("mock out the RegisterCommandDispatcher method")
// 			},
// 			RegisterDBConnectionFunc: func(name string, connection *sql.DB)  {
// 				panic("mock out the RegisterDBConnection method")
// 			},
// 			RegisterEntityFactoryFunc: func(name string, factory model.EntityFactory)  {
// 				panic("mock out the RegisterEntityFactory method")
// 			},
// 			RegisterEntityRepositoryFunc: func(name string, repository model.EntityRepository)  {
// 				panic("mock out the RegisterEntityRepository method")
// 			},
// 			RegisterEventStoreFunc: func(name string, repository model.EventRepository)  {
// 				panic("mock out the RegisterEventStore method")
// 			},
// 			RegisterGORMDBFunc: func(name string, connection *gorm.DB)  {
// 				panic("mock out the RegisterGORMDB method")
// 			},
// 			RegisterHTTPClientFunc: func(name string, client *http.Client)  {
// 				panic("mock out the RegisterHTTPClient method")
// 			},
// 			RegisterLogFunc: func(name string, logger model.Log)  {
// 				panic("mock out the RegisterLog method")
// 			},
// 			RegisterPermissionEnforcerFunc: func(name string, enforcer *casbin.Enforcer)  {
// 				panic("mock out the RegisterPermissionEnforcer method")
// 			},
// 			RegisterProjectionFunc: func(name string, projection model.Projection)  {
// 				panic("mock out the RegisterProjection method")
// 			},
// 		}
//
// 		// use mockedContainer in code that requires model.Container
// 		// and then make assertions.
//
// 	}
type ContainerMock struct {
	// GetCommandDispatcherFunc mocks the GetCommandDispatcher method.
	GetCommandDispatcherFunc func(name string) (model.CommandDispatcher, error)

	// GetConfigFunc mocks the GetConfig method.
	GetConfigFunc func() *openapi3.Swagger

	// GetDBConnectionFunc mocks the GetDBConnection method.
	GetDBConnectionFunc func(name string) (*sql.DB, error)

	// GetEntityFactoriesFunc mocks the GetEntityFactories method.
	GetEntityFactoriesFunc func() map[string]model.EntityFactory

	// GetEntityFactoryFunc mocks the GetEntityFactory method.
	GetEntityFactoryFunc func(name string) (model.EntityFactory, error)

	// GetEntityRepositoryFunc mocks the GetEntityRepository method.
	GetEntityRepositoryFunc func(name string) (model.EntityRepository, error)

	// GetEventStoreFunc mocks the GetEventStore method.
	GetEventStoreFunc func(name string) (model.EventRepository, error)

	// GetGormDBConnectionFunc mocks the GetGormDBConnection method.
	GetGormDBConnectionFunc func(name string) (*gorm.DB, error)

	// GetHTTPClientFunc mocks the GetHTTPClient method.
	GetHTTPClientFunc func(name string) (*http.Client, error)

	// GetLogFunc mocks the GetLog method.
	GetLogFunc func(name string) (model.Log, error)

	// GetPermissionEnforcerFunc mocks the GetPermissionEnforcer method.
	GetPermissionEnforcerFunc func(name string) (*casbin.Enforcer, error)

	// GetProjectionFunc mocks the GetProjection method.
	GetProjectionFunc func(name string) (model.Projection, error)

	// RegisterCommandDispatcherFunc mocks the RegisterCommandDispatcher method.
	RegisterCommandDispatcherFunc func(name string, dispatcher model.CommandDispatcher)

	// RegisterDBConnectionFunc mocks the RegisterDBConnection method.
	RegisterDBConnectionFunc func(name string, connection *sql.DB)

	// RegisterEntityFactoryFunc mocks the RegisterEntityFactory method.
	RegisterEntityFactoryFunc func(name string, factory model.EntityFactory)

	// RegisterEntityRepositoryFunc mocks the RegisterEntityRepository method.
	RegisterEntityRepositoryFunc func(name string, repository model.EntityRepository)

	// RegisterEventStoreFunc mocks the RegisterEventStore method.
	RegisterEventStoreFunc func(name string, repository model.EventRepository)

	// RegisterGORMDBFunc mocks the RegisterGORMDB method.
	RegisterGORMDBFunc func(name string, connection *gorm.DB)

	// RegisterHTTPClientFunc mocks the RegisterHTTPClient method.
	RegisterHTTPClientFunc func(name string, client *http.Client)

	// RegisterLogFunc mocks the RegisterLog method.
	RegisterLogFunc func(name string, logger model.Log)

	// RegisterPermissionEnforcerFunc mocks the RegisterPermissionEnforcer method.
	RegisterPermissionEnforcerFunc func(name string, enforcer *casbin.Enforcer)

	// RegisterProjectionFunc mocks the RegisterProjection method.
	RegisterProjectionFunc func(name string, projection model.Projection)

	// calls tracks calls to the methods.
	calls struct {
		// GetCommandDispatcher holds details about calls to the GetCommandDispatcher method.
		GetCommandDispatcher []struct {
			// Name is the name argument value.
			Name string
		}
		// GetConfig holds details about calls to the GetConfig method.
		GetConfig []struct {
		}
		// GetDBConnection holds details about calls to the GetDBConnection method.
		GetDBConnection []struct {
			// Name is the name argument value.
			Name string
		}
		// GetEntityFactories holds details about calls to the GetEntityFactories method.
		GetEntityFactories []struct {
		}
		// GetEntityFactory holds details about calls to the GetEntityFactory method.
		GetEntityFactory []struct {
			// Name is the name argument value.
			Name string
		}
		// GetEntityRepository holds details about calls to the GetEntityRepository method.
		GetEntityRepository []struct {
			// Name is the name argument value.
			Name string
		}
		// GetEventStore holds details about calls to the GetEventStore method.
		GetEventStore []struct {
			// Name is the name argument value.
			Name string
		}
		// GetGormDBConnection holds details about calls to the GetGormDBConnection method.
		GetGormDBConnection []struct {
			// Name is the name argument value.
			Name string
		}
		// GetHTTPClient holds details about calls to the GetHTTPClient method.
		GetHTTPClient []struct {
			// Name is the name argument value.
			Name string
		}
		// GetLog holds details about calls to the GetLog method.
		GetLog []struct {
			// Name is the name argument value.
			Name string
		}
		// GetPermissionEnforcer holds details about calls to the GetPermissionEnforcer method.
		GetPermissionEnforcer []struct {
			// Name is the name argument value.
			Name string
		}
		// GetProjection holds details about calls to the GetProjection method.
		GetProjection []struct {
			// Name is the name argument value.
			Name string
		}
		// RegisterCommandDispatcher holds details about calls to the RegisterCommandDispatcher method.
		RegisterCommandDispatcher []struct {
			// Name is the name argument value.
			Name string
			// Dispatcher is the dispatcher argument value.
			Dispatcher model.CommandDispatcher
		}
		// RegisterDBConnection holds details about calls to the RegisterDBConnection method.
		RegisterDBConnection []struct {
			// Name is the name argument value.
			Name string
			// Connection is the connection argument value.
			Connection *sql.DB
		}
		// RegisterEntityFactory holds details about calls to the RegisterEntityFactory method.
		RegisterEntityFactory []struct {
			// Name is the name argument value.
			Name string
			// Factory is the factory argument value.
			Factory model.EntityFactory
		}
		// RegisterEntityRepository holds details about calls to the RegisterEntityRepository method.
		RegisterEntityRepository []struct {
			// Name is the name argument value.
			Name string
			// Repository is the repository argument value.
			Repository model.EntityRepository
		}
		// RegisterEventStore holds details about calls to the RegisterEventStore method.
		RegisterEventStore []struct {
			// Name is the name argument value.
			Name string
			// Repository is the repository argument value.
			Repository model.EventRepository
		}
		// RegisterGORMDB holds details about calls to the RegisterGORMDB method.
		RegisterGORMDB []struct {
			// Name is the name argument value.
			Name string
			// Connection is the connection argument value.
			Connection *gorm.DB
		}
		// RegisterHTTPClient holds details about calls to the RegisterHTTPClient method.
		RegisterHTTPClient []struct {
			// Name is the name argument value.
			Name string
			// Client is the client argument value.
			Client *http.Client
		}
		// RegisterLog holds details about calls to the RegisterLog method.
		RegisterLog []struct {
			// Name is the name argument value.
			Name string
			// Logger is the logger argument value.
			Logger model.Log
		}
		// RegisterPermissionEnforcer holds details about calls to the RegisterPermissionEnforcer method.
		RegisterPermissionEnforcer []struct {
			// Name is the name argument value.
			Name string
			// Enforcer is the enforcer argument value.
			Enforcer *casbin.Enforcer
		}
		// RegisterProjection holds details about calls to the RegisterProjection method.
		RegisterProjection []struct {
			// Name is the name argument value.
			Name string
			// Projection is the projection argument value.
			Projection model.Projection
		}
	}
	lockGetCommandDispatcher       sync.RWMutex
	lockGetConfig                  sync.RWMutex
	lockGetDBConnection            sync.RWMutex
	lockGetEntityFactories         sync.RWMutex
	lockGetEntityFactory           sync.RWMutex
	lockGetEntityRepository        sync.RWMutex
	lockGetEventStore              sync.RWMutex
	lockGetGormDBConnection        sync.RWMutex
	lockGetHTTPClient              sync.RWMutex
	lockGetLog                     sync.RWMutex
	lockGetPermissionEnforcer      sync.RWMutex
	lockGetProjection              sync.RWMutex
	lockRegisterCommandDispatcher  sync.RWMutex
	lockRegisterDBConnection       sync.RWMutex
	lockRegisterEntityFactory      sync.RWMutex
	lockRegisterEntityRepository   sync.RWMutex
	lockRegisterEventStore         sync.RWMutex
	lockRegisterGORMDB             sync.RWMutex
	lockRegisterHTTPClient         sync.RWMutex
	lockRegisterLog                sync.RWMutex
	lockRegisterPermissionEnforcer sync.RWMutex
	lockRegisterProjection         sync.RWMutex
}

// GetCommandDispatcher calls GetCommandDispatcherFunc.
func (mock *ContainerMock) GetCommandDispatcher(name string) (model.CommandDispatcher, error) {
	if mock.GetCommandDispatcherFunc == nil {
		panic("ContainerMock.GetCommandDispatcherFunc: method is nil but Container.GetCommandDispatcher was just called")
	}
	callInfo := struct {
		Name string
	}{
		Name: name,
	}
	mock.lockGetCommandDispatcher.Lock()
	mock.calls.GetCommandDispatcher = append(mock.calls.GetCommandDispatcher, callInfo)
	mock.lockGetCommandDispatcher.Unlock()
	return mock.GetCommandDispatcherFunc(name)
}

// GetCommandDispatcherCalls gets all the calls that were made to GetCommandDispatcher.
// Check the length with:
//     len(mockedContainer.GetCommandDispatcherCalls())
func (mock *ContainerMock) GetCommandDispatcherCalls() []struct {
	Name string
} {
	var calls []struct {
		Name string
	}
	mock.lockGetCommandDispatcher.RLock()
	calls = mock.calls.GetCommandDispatcher
	mock.lockGetCommandDispatcher.RUnlock()
	return calls
}

// GetConfig calls GetConfigFunc.
func (mock *ContainerMock) GetConfig() *openapi3.Swagger {
	if mock.GetConfigFunc == nil {
		panic("ContainerMock.GetConfigFunc: method is nil but Container.GetConfig was just called")
	}
	callInfo := struct {
	}{}
	mock.lockGetConfig.Lock()
	mock.calls.GetConfig = append(mock.calls.GetConfig, callInfo)
	mock.lockGetConfig.Unlock()
	return mock.GetConfigFunc()
}

// GetConfigCalls gets all the calls that were made to GetConfig.
// Check the length with:
//     len(mockedContainer.GetConfigCalls())
func (mock *ContainerMock) GetConfigCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockGetConfig.RLock()
	calls = mock.calls.GetConfig
	mock.lockGetConfig.RUnlock()
	return calls
}

// GetDBConnection calls GetDBConnectionFunc.
func (mock *ContainerMock) GetDBConnection(name string) (*sql.DB, error) {
	if mock.GetDBConnectionFunc == nil {
		panic("ContainerMock.GetDBConnectionFunc: method is nil but Container.GetDBConnection was just called")
	}
	callInfo := struct {
		Name string
	}{
		Name: name,
	}
	mock.lockGetDBConnection.Lock()
	mock.calls.GetDBConnection = append(mock.calls.GetDBConnection, callInfo)
	mock.lockGetDBConnection.Unlock()
	return mock.GetDBConnectionFunc(name)
}

// GetDBConnectionCalls gets all the calls that were made to GetDBConnection.
// Check the length with:
//     len(mockedContainer.GetDBConnectionCalls())
func (mock *ContainerMock) GetDBConnectionCalls() []struct {
	Name string
} {
	var calls []struct {
		Name string
	}
	mock.lockGetDBConnection.RLock()
	calls = mock.calls.GetDBConnection
	mock.lockGetDBConnection.RUnlock()
	return calls
}

// GetEntityFactories calls GetEntityFactoriesFunc.
func (mock *ContainerMock) GetEntityFactories() map[string]model.EntityFactory {
	if mock.GetEntityFactoriesFunc == nil {
		panic("ContainerMock.GetEntityFactoriesFunc: method is nil but Container.GetEntityFactories was just called")
	}
	callInfo := struct {
	}{}
	mock.lockGetEntityFactories.Lock()
	mock.calls.GetEntityFactories = append(mock.calls.GetEntityFactories, callInfo)
	mock.lockGetEntityFactories.Unlock()
	return mock.GetEntityFactoriesFunc()
}

// GetEntityFactoriesCalls gets all the calls that were made to GetEntityFactories.
// Check the length with:
//     len(mockedContainer.GetEntityFactoriesCalls())
func (mock *ContainerMock) GetEntityFactoriesCalls() []struct {
} {
	var calls []struct {
	}
	mock.lockGetEntityFactories.RLock()
	calls = mock.calls.GetEntityFactories
	mock.lockGetEntityFactories.RUnlock()
	return calls
}

// GetEntityFactory calls GetEntityFactoryFunc.
func (mock *ContainerMock) GetEntityFactory(name string) (model.EntityFactory, error) {
	if mock.GetEntityFactoryFunc == nil {
		panic("ContainerMock.GetEntityFactoryFunc: method is nil but Container.GetEntityFactory was just called")
	}
	callInfo := struct {
		Name string
	}{
		Name: name,
	}
	mock.lockGetEntityFactory.Lock()
	mock.calls.GetEntityFactory = append(mock.calls.GetEntityFactory, callInfo)
	mock.lockGetEntityFactory.Unlock()
	return mock.GetEntityFactoryFunc(name)
}

// GetEntityFactoryCalls gets all the calls that were made to GetEntityFactory.
// Check the length with:
//     len(mockedContainer.GetEntityFactoryCalls())
func (mock *ContainerMock) GetEntityFactoryCalls() []struct {
	Name string
} {
	var calls []struct {
		Name string
	}
	mock.lockGetEntityFactory.RLock()
	calls = mock.calls.GetEntityFactory
	mock.lockGetEntityFactory.RUnlock()
	return calls
}

// GetEntityRepository calls GetEntityRepositoryFunc.
func (mock *ContainerMock) GetEntityRepository(name string) (model.EntityRepository, error) {
	if mock.GetEntityRepositoryFunc == nil {
		panic("ContainerMock.GetEntityRepositoryFunc: method is nil but Container.GetEntityRepository was just called")
	}
	callInfo := struct {
		Name string
	}{
		Name: name,
	}
	mock.lockGetEntityRepository.Lock()
	mock.calls.GetEntityRepository = append(mock.calls.GetEntityRepository, callInfo)
	mock.lockGetEntityRepository.Unlock()
	return mock.GetEntityRepositoryFunc(name)
}

// GetEntityRepositoryCalls gets all the calls that were made to GetEntityRepository.
// Check the length with:
//     len(mockedContainer.GetEntityRepositoryCalls())
func (mock *ContainerMock) GetEntityRepositoryCalls() []struct {
	Name string
} {
	var calls []struct {
		Name string
	}
	mock.lockGetEntityRepository.RLock()
	calls = mock.calls.GetEntityRepository
	mock.lockGetEntityRepository.RUnlock()
	return calls
}

// GetEventStore calls GetEventStoreFunc.
func (mock *ContainerMock) GetEventStore(name string) (model.EventRepository, error) {
	if mock.GetEventStoreFunc == nil {
		panic("ContainerMock.GetEventStoreFunc: method is nil but Container.GetEventStore was just called")
	}
	callInfo := struct {
		Name string
	}{
		Name: name,
	}
	mock.lockGetEventStore.Lock()
	mock.calls.GetEventStore = append(mock.calls.GetEventStore, callInfo)
	mock.lockGetEventStore.Unlock()
	return mock.GetEventStoreFunc(name)
}

// GetEventStoreCalls gets all the calls that were made to GetEventStore.
// Check the length with:
//     len(mockedContainer.GetEventStoreCalls())
func (mock *ContainerMock) GetEventStoreCalls() []struct {
	Name string
} {
	var calls []struct {
		Name string
	}
	mock.lockGetEventStore.RLock()
	calls = mock.calls.GetEventStore
	mock.lockGetEventStore.RUnlock()
	return calls
}

// GetGormDBConnection calls GetGormDBConnectionFunc.
func (mock *ContainerMock) GetGormDBConnection(name string) (*gorm.DB, error) {
	if mock.GetGormDBConnectionFunc == nil {
		panic("ContainerMock.GetGormDBConnectionFunc: method is nil but Container.GetGormDBConnection was just called")
	}
	callInfo := struct {
		Name string
	}{
		Name: name,
	}
	mock.lockGetGormDBConnection.Lock()
	mock.calls.GetGormDBConnection = append(mock.calls.GetGormDBConnection, callInfo)
	mock.lockGetGormDBConnection.Unlock()
	return mock.GetGormDBConnectionFunc(name)
}

// GetGormDBConnectionCalls gets all the calls that were made to GetGormDBConnection.
// Check the length with:
//     len(mockedContainer.GetGormDBConnectionCalls())
func (mock *ContainerMock) GetGormDBConnectionCalls() []struct {
	Name string
} {
	var calls []struct {
		Name string
	}
	mock.lockGetGormDBConnection.RLock()
	calls = mock.calls.GetGormDBConnection
	mock.lockGetGormDBConnection.RUnlock()
	return calls
}

// GetHTTPClient calls GetHTTPClientFunc.
func (mock *ContainerMock) GetHTTPClient(name string) (*http.Client, error) {
	if mock.GetHTTPClientFunc == nil {
		panic("ContainerMock.GetHTTPClientFunc: method is nil but Container.GetHTTPClient was just called")
	}
	callInfo := struct {
		Name string
	}{
		Name: name,
	}
	mock.lockGetHTTPClient.Lock()
	mock.calls.GetHTTPClient = append(mock.calls.GetHTTPClient, callInfo)
	mock.lockGetHTTPClient.Unlock()
	return mock.GetHTTPClientFunc(name)
}

// GetHTTPClientCalls gets all the calls that were made to GetHTTPClient.
// Check the length with:
//     len(mockedContainer.GetHTTPClientCalls())
func (mock *ContainerMock) GetHTTPClientCalls() []struct {
	Name string
} {
	var calls []struct {
		Name string
	}
	mock.lockGetHTTPClient.RLock()
	calls = mock.calls.GetHTTPClient
	mock.lockGetHTTPClient.RUnlock()
	return calls
}

// GetLog calls GetLogFunc.
func (mock *ContainerMock) GetLog(name string) (model.Log, error) {
	if mock.GetLogFunc == nil {
		panic("ContainerMock.GetLogFunc: method is nil but Container.GetLog was just called")
	}
	callInfo := struct {
		Name string
	}{
		Name: name,
	}
	mock.lockGetLog.Lock()
	mock.calls.GetLog = append(mock.calls.GetLog, callInfo)
	mock.lockGetLog.Unlock()
	return mock.GetLogFunc(name)
}

// GetLogCalls gets all the calls that were made to GetLog.
// Check the length with:
//     len(mockedContainer.GetLogCalls())
func (mock *ContainerMock) GetLogCalls() []struct {
	Name string
} {
	var calls []struct {
		Name string
	}
	mock.lockGetLog.RLock()
	calls = mock.calls.GetLog
	mock.lockGetLog.RUnlock()
	return calls
}

// GetPermissionEnforcer calls GetPermissionEnforcerFunc.
func (mock *ContainerMock) GetPermissionEnforcer(name string) (*casbin.Enforcer, error) {
	if mock.GetPermissionEnforcerFunc == nil {
		panic("ContainerMock.GetPermissionEnforcerFunc: method is nil but Container.GetPermissionEnforcer was just called")
	}
	callInfo := struct {
		Name string
	}{
		Name: name,
	}
	mock.lockGetPermissionEnforcer.Lock()
	mock.calls.GetPermissionEnforcer = append(mock.calls.GetPermissionEnforcer, callInfo)
	mock.lockGetPermissionEnforcer.Unlock()
	return mock.GetPermissionEnforcerFunc(name)
}

// GetPermissionEnforcerCalls gets all the calls that were made to GetPermissionEnforcer.
// Check the length with:
//     len(mockedContainer.GetPermissionEnforcerCalls())
func (mock *ContainerMock) GetPermissionEnforcerCalls() []struct {
	Name string
} {
	var calls []struct {
		Name string
	}
	mock.lockGetPermissionEnforcer.RLock()
	calls = mock.calls.GetPermissionEnforcer
	mock.lockGetPermissionEnforcer.RUnlock()
	return calls
}

func (mock *ContainerMock) GetGormDB() *gorm.DB {
	return mock.GetGormDB()
}

// GetProjection calls GetProjectionFunc.
func (mock *ContainerMock) GetProjection(name string) (model.Projection, error) {
	if mock.GetProjectionFunc == nil {
		panic("ContainerMock.GetProjectionFunc: method is nil but Container.GetProjection was just called")
	}
	callInfo := struct {
		Name string
	}{
		Name: name,
	}
	mock.lockGetProjection.Lock()
	mock.calls.GetProjection = append(mock.calls.GetProjection, callInfo)
	mock.lockGetProjection.Unlock()
	return mock.GetProjectionFunc(name)
}

// GetProjectionCalls gets all the calls that were made to GetProjection.
// Check the length with:
//     len(mockedContainer.GetProjectionCalls())
func (mock *ContainerMock) GetProjectionCalls() []struct {
	Name string
} {
	var calls []struct {
		Name string
	}
	mock.lockGetProjection.RLock()
	calls = mock.calls.GetProjection
	mock.lockGetProjection.RUnlock()
	return calls
}

// RegisterCommandDispatcher calls RegisterCommandDispatcherFunc.
func (mock *ContainerMock) RegisterCommandDispatcher(name string, dispatcher model.CommandDispatcher) {
	if mock.RegisterCommandDispatcherFunc == nil {
		panic("ContainerMock.RegisterCommandDispatcherFunc: method is nil but Container.RegisterCommandDispatcher was just called")
	}
	callInfo := struct {
		Name       string
		Dispatcher model.CommandDispatcher
	}{
		Name:       name,
		Dispatcher: dispatcher,
	}
	mock.lockRegisterCommandDispatcher.Lock()
	mock.calls.RegisterCommandDispatcher = append(mock.calls.RegisterCommandDispatcher, callInfo)
	mock.lockRegisterCommandDispatcher.Unlock()
	mock.RegisterCommandDispatcherFunc(name, dispatcher)
}

// RegisterCommandDispatcherCalls gets all the calls that were made to RegisterCommandDispatcher.
// Check the length with:
//     len(mockedContainer.RegisterCommandDispatcherCalls())
func (mock *ContainerMock) RegisterCommandDispatcherCalls() []struct {
	Name       string
	Dispatcher model.CommandDispatcher
} {
	var calls []struct {
		Name       string
		Dispatcher model.CommandDispatcher
	}
	mock.lockRegisterCommandDispatcher.RLock()
	calls = mock.calls.RegisterCommandDispatcher
	mock.lockRegisterCommandDispatcher.RUnlock()
	return calls
}

// RegisterDBConnection calls RegisterDBConnectionFunc.
func (mock *ContainerMock) RegisterDBConnection(name string, connection *sql.DB) {
	if mock.RegisterDBConnectionFunc == nil {
		panic("ContainerMock.RegisterDBConnectionFunc: method is nil but Container.RegisterDBConnection was just called")
	}
	callInfo := struct {
		Name       string
		Connection *sql.DB
	}{
		Name:       name,
		Connection: connection,
	}
	mock.lockRegisterDBConnection.Lock()
	mock.calls.RegisterDBConnection = append(mock.calls.RegisterDBConnection, callInfo)
	mock.lockRegisterDBConnection.Unlock()
	mock.RegisterDBConnectionFunc(name, connection)
}

// RegisterDBConnectionCalls gets all the calls that were made to RegisterDBConnection.
// Check the length with:
//     len(mockedContainer.RegisterDBConnectionCalls())
func (mock *ContainerMock) RegisterDBConnectionCalls() []struct {
	Name       string
	Connection *sql.DB
} {
	var calls []struct {
		Name       string
		Connection *sql.DB
	}
	mock.lockRegisterDBConnection.RLock()
	calls = mock.calls.RegisterDBConnection
	mock.lockRegisterDBConnection.RUnlock()
	return calls
}

// RegisterEntityFactory calls RegisterEntityFactoryFunc.
func (mock *ContainerMock) RegisterEntityFactory(name string, factory model.EntityFactory) {
	if mock.RegisterEntityFactoryFunc == nil {
		panic("ContainerMock.RegisterEntityFactoryFunc: method is nil but Container.RegisterEntityFactory was just called")
	}
	callInfo := struct {
		Name    string
		Factory model.EntityFactory
	}{
		Name:    name,
		Factory: factory,
	}
	mock.lockRegisterEntityFactory.Lock()
	mock.calls.RegisterEntityFactory = append(mock.calls.RegisterEntityFactory, callInfo)
	mock.lockRegisterEntityFactory.Unlock()
	mock.RegisterEntityFactoryFunc(name, factory)
}

// RegisterEntityFactoryCalls gets all the calls that were made to RegisterEntityFactory.
// Check the length with:
//     len(mockedContainer.RegisterEntityFactoryCalls())
func (mock *ContainerMock) RegisterEntityFactoryCalls() []struct {
	Name    string
	Factory model.EntityFactory
} {
	var calls []struct {
		Name    string
		Factory model.EntityFactory
	}
	mock.lockRegisterEntityFactory.RLock()
	calls = mock.calls.RegisterEntityFactory
	mock.lockRegisterEntityFactory.RUnlock()
	return calls
}

// RegisterEntityRepository calls RegisterEntityRepositoryFunc.
func (mock *ContainerMock) RegisterEntityRepository(name string, repository model.EntityRepository) {
	if mock.RegisterEntityRepositoryFunc == nil {
		panic("ContainerMock.RegisterEntityRepositoryFunc: method is nil but Container.RegisterEntityRepository was just called")
	}
	callInfo := struct {
		Name       string
		Repository model.EntityRepository
	}{
		Name:       name,
		Repository: repository,
	}
	mock.lockRegisterEntityRepository.Lock()
	mock.calls.RegisterEntityRepository = append(mock.calls.RegisterEntityRepository, callInfo)
	mock.lockRegisterEntityRepository.Unlock()
	mock.RegisterEntityRepositoryFunc(name, repository)
}

// RegisterEntityRepositoryCalls gets all the calls that were made to RegisterEntityRepository.
// Check the length with:
//     len(mockedContainer.RegisterEntityRepositoryCalls())
func (mock *ContainerMock) RegisterEntityRepositoryCalls() []struct {
	Name       string
	Repository model.EntityRepository
} {
	var calls []struct {
		Name       string
		Repository model.EntityRepository
	}
	mock.lockRegisterEntityRepository.RLock()
	calls = mock.calls.RegisterEntityRepository
	mock.lockRegisterEntityRepository.RUnlock()
	return calls
}

// RegisterEventStore calls RegisterEventStoreFunc.
func (mock *ContainerMock) RegisterEventStore(name string, repository model.EventRepository) {
	if mock.RegisterEventStoreFunc == nil {
		panic("ContainerMock.RegisterEventStoreFunc: method is nil but Container.RegisterEventStore was just called")
	}
	callInfo := struct {
		Name       string
		Repository model.EventRepository
	}{
		Name:       name,
		Repository: repository,
	}
	mock.lockRegisterEventStore.Lock()
	mock.calls.RegisterEventStore = append(mock.calls.RegisterEventStore, callInfo)
	mock.lockRegisterEventStore.Unlock()
	mock.RegisterEventStoreFunc(name, repository)
}

// RegisterEventStoreCalls gets all the calls that were made to RegisterEventStore.
// Check the length with:
//     len(mockedContainer.RegisterEventStoreCalls())
func (mock *ContainerMock) RegisterEventStoreCalls() []struct {
	Name       string
	Repository model.EventRepository
} {
	var calls []struct {
		Name       string
		Repository model.EventRepository
	}
	mock.lockRegisterEventStore.RLock()
	calls = mock.calls.RegisterEventStore
	mock.lockRegisterEventStore.RUnlock()
	return calls
}

// RegisterGORMDB calls RegisterGORMDBFunc.
func (mock *ContainerMock) RegisterGORMDB(name string, connection *gorm.DB) {
	if mock.RegisterGORMDBFunc == nil {
		panic("ContainerMock.RegisterGORMDBFunc: method is nil but Container.RegisterGORMDB was just called")
	}
	callInfo := struct {
		Name       string
		Connection *gorm.DB
	}{
		Name:       name,
		Connection: connection,
	}
	mock.lockRegisterGORMDB.Lock()
	mock.calls.RegisterGORMDB = append(mock.calls.RegisterGORMDB, callInfo)
	mock.lockRegisterGORMDB.Unlock()
	mock.RegisterGORMDBFunc(name, connection)
}

// RegisterGORMDBCalls gets all the calls that were made to RegisterGORMDB.
// Check the length with:
//     len(mockedContainer.RegisterGORMDBCalls())
func (mock *ContainerMock) RegisterGORMDBCalls() []struct {
	Name       string
	Connection *gorm.DB
} {
	var calls []struct {
		Name       string
		Connection *gorm.DB
	}
	mock.lockRegisterGORMDB.RLock()
	calls = mock.calls.RegisterGORMDB
	mock.lockRegisterGORMDB.RUnlock()
	return calls
}

// RegisterHTTPClient calls RegisterHTTPClientFunc.
func (mock *ContainerMock) RegisterHTTPClient(name string, client *http.Client) {
	if mock.RegisterHTTPClientFunc == nil {
		panic("ContainerMock.RegisterHTTPClientFunc: method is nil but Container.RegisterHTTPClient was just called")
	}
	callInfo := struct {
		Name   string
		Client *http.Client
	}{
		Name:   name,
		Client: client,
	}
	mock.lockRegisterHTTPClient.Lock()
	mock.calls.RegisterHTTPClient = append(mock.calls.RegisterHTTPClient, callInfo)
	mock.lockRegisterHTTPClient.Unlock()
	mock.RegisterHTTPClientFunc(name, client)
}

// RegisterHTTPClientCalls gets all the calls that were made to RegisterHTTPClient.
// Check the length with:
//     len(mockedContainer.RegisterHTTPClientCalls())
func (mock *ContainerMock) RegisterHTTPClientCalls() []struct {
	Name   string
	Client *http.Client
} {
	var calls []struct {
		Name   string
		Client *http.Client
	}
	mock.lockRegisterHTTPClient.RLock()
	calls = mock.calls.RegisterHTTPClient
	mock.lockRegisterHTTPClient.RUnlock()
	return calls
}

// RegisterLog calls RegisterLogFunc.
func (mock *ContainerMock) RegisterLog(name string, logger model.Log) {
	if mock.RegisterLogFunc == nil {
		panic("ContainerMock.RegisterLogFunc: method is nil but Container.RegisterLog was just called")
	}
	callInfo := struct {
		Name   string
		Logger model.Log
	}{
		Name:   name,
		Logger: logger,
	}
	mock.lockRegisterLog.Lock()
	mock.calls.RegisterLog = append(mock.calls.RegisterLog, callInfo)
	mock.lockRegisterLog.Unlock()
	mock.RegisterLogFunc(name, logger)
}

// RegisterLogCalls gets all the calls that were made to RegisterLog.
// Check the length with:
//     len(mockedContainer.RegisterLogCalls())
func (mock *ContainerMock) RegisterLogCalls() []struct {
	Name   string
	Logger model.Log
} {
	var calls []struct {
		Name   string
		Logger model.Log
	}
	mock.lockRegisterLog.RLock()
	calls = mock.calls.RegisterLog
	mock.lockRegisterLog.RUnlock()
	return calls
}

// RegisterPermissionEnforcer calls RegisterPermissionEnforcerFunc.
func (mock *ContainerMock) RegisterPermissionEnforcer(name string, enforcer *casbin.Enforcer) {
	if mock.RegisterPermissionEnforcerFunc == nil {
		panic("ContainerMock.RegisterPermissionEnforcerFunc: method is nil but Container.RegisterPermissionEnforcer was just called")
	}
	callInfo := struct {
		Name     string
		Enforcer *casbin.Enforcer
	}{
		Name:     name,
		Enforcer: enforcer,
	}
	mock.lockRegisterPermissionEnforcer.Lock()
	mock.calls.RegisterPermissionEnforcer = append(mock.calls.RegisterPermissionEnforcer, callInfo)
	mock.lockRegisterPermissionEnforcer.Unlock()
	mock.RegisterPermissionEnforcerFunc(name, enforcer)
}

// RegisterPermissionEnforcerCalls gets all the calls that were made to RegisterPermissionEnforcer.
// Check the length with:
//     len(mockedContainer.RegisterPermissionEnforcerCalls())
func (mock *ContainerMock) RegisterPermissionEnforcerCalls() []struct {
	Name     string
	Enforcer *casbin.Enforcer
} {
	var calls []struct {
		Name     string
		Enforcer *casbin.Enforcer
	}
	mock.lockRegisterPermissionEnforcer.RLock()
	calls = mock.calls.RegisterPermissionEnforcer
	mock.lockRegisterPermissionEnforcer.RUnlock()
	return calls
}

// RegisterProjection calls RegisterProjectionFunc.
func (mock *ContainerMock) RegisterProjection(name string, projection model.Projection) {
	if mock.RegisterProjectionFunc == nil {
		panic("ContainerMock.RegisterProjectionFunc: method is nil but Container.RegisterProjection was just called")
	}
	callInfo := struct {
		Name       string
		Projection model.Projection
	}{
		Name:       name,
		Projection: projection,
	}
	mock.lockRegisterProjection.Lock()
	mock.calls.RegisterProjection = append(mock.calls.RegisterProjection, callInfo)
	mock.lockRegisterProjection.Unlock()
	mock.RegisterProjectionFunc(name, projection)
}

// RegisterProjectionCalls gets all the calls that were made to RegisterProjection.
// Check the length with:
//     len(mockedContainer.RegisterProjectionCalls())
func (mock *ContainerMock) RegisterProjectionCalls() []struct {
	Name       string
	Projection model.Projection
} {
	var calls []struct {
		Name       string
		Projection model.Projection
	}
	mock.lockRegisterProjection.RLock()
	calls = mock.calls.RegisterProjection
	mock.lockRegisterProjection.RUnlock()
	return calls
}
