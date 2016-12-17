package main

func dogoLogs() {
	// =======================================================
	// This is a placeholder for a future 'dogo logs' command
	// for reading/searching/tailing log files across a given
	// set of servers/resources
	// =======================================================


	// Notes--------------------------------------------------
	// 
	// logs are hierical
	// environment.machine.<name>.<name>.<name>...
	// vagrant2.syslog
	// vagrant2.docker.memcached // exposed by docker module
	// vagrant2.docker.postgres // exposed by docker module
	// vagrant2.firewall // exposed by firewall

	// so you can ask for
	// docker logs -t <match> <search>

	// where -t means: tail!

	// where <match> can be
	//* // all logs
	//*.docker.* // all docker logs from all machiens
	//memcached // anything that matches memcached

	// where <search> is anything that would go into grep
}
