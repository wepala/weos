# WeOS  Microservice


## Quickstart 

APIs can be run on a local machine or deployed to a remote server. 

* Download the release appropriate for your platform
* Update api.yaml with the database configuration (by default the api will use sqlite)
* Run binary (you can run specify which port to use using the `-port` switch on the command line)

## Project layout
    api.yaml    # API Configuration File
    .env.dist   # Copy this file to create an environment variable file (.env)
    src/
        api.go  # API Handlers 
        dtos.go # Data transfer structs that map to the components in the api spec
    
    projections/ # Projections package
        projections.go # Projection interface that all projections must impelment
        gorm.go # GORM projection implementation
    
    ...       # Other test files and project files
