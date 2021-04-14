package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/schema"
)

func dogoBuild(config *schema.Config) {
	alreadyDoing := make(map[string]bool)
	buildTasks := commandtree.NewRootCommand("Building Components")
	for _, pack := range config.Packages {
		for _, mod := range pack.Modules {
			if mod.ModuleName == "docker" {
				if image, found := mod.Config["image"]; found {
					if str, ok := image.(string); ok {
						key := "image:" + str
						if _, found := alreadyDoing[key]; !found {
							buildTasks.Add("docker pull '"+str+"'", commandtree.NewExecCommand("", " -> pulling docker image took %v", "", "docker", "pull", str))
							alreadyDoing[key] = true
						}
					}
				}
				if folder, found := mod.Config["folder"]; found {
					if str, ok := folder.(string); ok {
						key := "folder:" + str
						if _, found := alreadyDoing[key]; !found {
							buildTasks.Add(str, commandtree.NewFuncCommand(func(c *commandtree.Command) {
								// valid path?
								path, err := filepath.Abs(str)
								if err != nil {
									c.Errf("Invalid path: '%v'. (%v)", path, err.Error())
									return
								}

								// does folder exist?
								if _, err := os.Stat(path); err != nil {
									if os.IsNotExist(err) {
										c.Errf("path: '%v' does not exist", path)
									} else {
										c.Errf("error for '%v': %v", path, err.Error())
									}
									return
								}

								// does folder have build script?
								buildScriptPath := filepath.Join(path, "build.sh")
								s, err := os.Stat(buildScriptPath)
								if err == nil {
									if s.Mode()&0111 == 0 {
										c.Errf("Found %v but it does not have the executable bit in filemode. Run 'chmod +x build.sh' to fix.", buildScriptPath)
										return
									}

									c.Logf("Running build script")
									start := time.Now()
									if err := commandtree.OSExec(c, path, "", buildScriptPath); err != nil {
										c.Err(err)
										return
									}
									c.Logf(" -> build script took %v", time.Since(start))
								}

								// docker build
								name := filepath.Base(path)
								c.Logf("Building docker image with tag='%v'", name)
								start := time.Now()

								if err = commandtree.OSExec(c, path, "", "docker", "build", "--progress", "plain", "-t", name, "."); err != nil {
									c.Err(err)
									return
								}
								c.Logf(" -> building docker image took %v", time.Since(start))
							}))
							alreadyDoing[key] = true
						}
					}
				}
			}
		}
	}

	// Run!
	r := commandtree.NewRunner(buildTasks, 5)
	go r.Run(nil)
	commandtree.ConsoleUI(buildTasks)
}
