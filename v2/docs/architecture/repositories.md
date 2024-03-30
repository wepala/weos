# Repositories

Repositories are used to store data in a persistent manner. Repositories must implement the Repository interface. 
A [repository](https://gorm.io/) based on GORM is provided by default. 


## Persist
The persist method on a repository is used to store data in the repository. Different databases have capabilities  that
can be used to enforce business rules e.g. unique constraints. Because not all databases implement the functionality
consistently the persist method returns an error when these constraints fail. The developer should supplement the functionalitly
in the repository when the intended database does not natively support it. 