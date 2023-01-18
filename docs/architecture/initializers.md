# Initializers 

Initializers are functions that are executed during the startup process of the api. Initializers are used to setup the 
default middleware, controllers, and routes of the api. Initializers can be overwritten by the user or new initializers
can be added to the service container. There are 3 types of initializers
1. Global Initializers - These are executed before any other initializers.
2. Path Initializers - These initializers are executed everytime a path is **processed** during the api setup
3. Operation Initializers - These are executed for every operation on a path during the api setup 

## Global Initializers

## Path Initializers

## Operation Initializers