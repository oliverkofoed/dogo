Dogo is an opinionated and focused development and deployment tool, that tries to make the development and deployment cycles for multi tiered projects as simple and easy as possible.

Dogo shines when you're a single developer or a small team just tring to get things done, and want a seamless progression from local development to production deployment while remaining in control.

A project in Dogo consists of multiple components, each of which is simply a Docker image, and one or more configuration files describing how the components should be connected in a live environment. The entire project is contained in a single folder for simplicty, which is usually kept in source control as a single unit.

Dogo is inspired and overlaps with tools and services such as Ansible and Kubernetes, but does not try to reach as wide as either of those, instead focusing on ease of use and simplicity.

Dogo is a single lightweight executable built in Go, with no external dependencies. All you need to manage a remote server is SSH access. You don't need to configure or install anything on the remote system. Dogo copies itself to the remote system in order to manage the state on the remote system. 

Dogo is also optimized for speed, by parallizing operations across multiple servers and bulking operations to minimize roundtrips. The key optimization is that the desired state (docker, firewall, etc) for each server is generated locally and only the commands needed to modify the remote state are transfered (in one go) to the remote system. This also means deployment to a system that is already fully deployed takes almost zero time, because it only requires a single roundtrip to each server which is done in parallel.

Lastly, Dogo avoids the usage of Docker hub or other Docker image registry facilities when transferering images to remote servers. Instead Dogo copies the image from the machine running dogo to the remote server if the remote server does not already have that exact image. This is done in the name of simplicity, to avoid having to deal with remote registries or managmenet of a private registry.

Usage
=====
With Dogo you describe your infrastructure and components and how they connect, and use Dogo to setup your local development environment or deploy remote environments such as your production or test environments.

A component is simply a Docker container.

A typical Dogo project has the following folder structure

	components/
		/webserver/
			Dockerfile
			build.sh
		/database/
			Dockerfile
	infrastructure.dconf
	
Typical development consists of checking out a repository, changing into the directory and running "dogo dev deploy". This will build the docker images and start the containers connected to each other.

Commands
--------
Simply change your working directory to a project folder and run any of the following commands to use Dogo.

'dogo build': 
	Build a Docker image for each component.

'dogo [env] deploy': 
	Deploys the [env] environment

[ TO BE DEVELOPED ]
'dogo [env] connect
	Starts the web management interface for the given environment and runs local tunnels for easy access to the components of the [env] environment while the command is running.

[ TO BE DEVELOPED ]
'dogo [env] ssh [servertag|servertag.component]'
	Starts an ssh connection to one of the servers of the environment that has the given tag. If a component is given, the ssh connection is established into the container running on the server.

[ TO BE DEVELOPED ]
'dogo [env] logs [servertag|servertag.component]'
	should allow outputting logs, grepping logs and tailing logs.
	
'dogo version': 
	Get the current Dogo version
	
Configuration
-------------
Dogo uses the excellent (HashiCorp Configuration Language)[https://github.com/hashicorp/hcl] for configuration syntax. 

Dogo reads all *.dconf files from the working directory into a unified configuration, so you can have either one "infrastructure.dconf" that covers everything or multiple files ("prod.dconf","dev.dconf","structure.dconf") as desired. We recommend starting with a single file though, to keep things simple.

[TODO: Syntax and structure of configuration file]