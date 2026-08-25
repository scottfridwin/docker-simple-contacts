I want to define a complete spec for a project that I am going to start. It will be driven almost entirely by an AI agent, so I want to make sure I spell out all of the details that I can. Please review the following information and provide feedback that I can apply before initially prompting the coding agent
 
 
Overview:

This will be a self-hosted contact management system with a database backend and RESTful api. Additionally there will be a PWA frontend to allow users to edit the data. The intent is to provide a relatively basic but functional contact management system that does not rely on a cloud provider. 
 
 
 
Design Statements:

- The primary language of the backend will be Go. 
- The project will be hosted on github and should include appropriate README and license files. 
- Development will take place in a dev container using VS Code. The project should include appropriate files to define this dev container and all necessary prerequisites. 
- Github workflows will be used for verisoning and release management. An example will be provided in order to build an appropriate workflow for this project. 
- Strict linting rules will be defined in the repository and will be enforced during builds. 
- Any external dependencies will be defined such that "Renovate" can automatically create changes and pull requests as necessary. 
- The output of the project is a container image; there is no intention of providing a standalone installation of the project (at least for now). 
- The backend database will be a separate hosted docker container running Postgresql. The project should allow for setting the host, port, user, and password as environment variables into the docker container. In the case of password, a file path is also allowed in order to support docker secrets. The project should use a single database with multiple tables if necessary. The name of the database can be an optional environment variable that defaults to "postgres". 
- The application is intended to be hosted behind a reverse proxy with authentication/authorization. There is no built-in authentication/authorization mechanism. Adding support for SSO via Authentik will be a 2.0 feature enhancement. 
- For the first iteration we only want to consider Person records. A Person will have a set of pre-defined standard fields but will also have room for unstructured fields that can be defined by the user. For example, in addition to the name "Scott Fridlund" my record could also have a custom value for "Blood Type". 
