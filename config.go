package main

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/hashicorp/hcl"
	"github.com/oliverkofoed/dogo/constructor"
	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/registry"
	"github.com/oliverkofoed/dogo/schema"
)

func addError(errors *[]error, location string, msg string, args ...interface{}) {
	*errors = append(*errors, neaterror.New(map[string]interface{}{
		"!location": location,
	}, msg, args...))
}

func addErrors(errors *[]error, location string, errs []error) {
	for _, e := range errs {
		e = neaterror.Enrich(e, map[string]interface{}{"!location": location})
		*errors = append(*errors, e)
	}
}

func buildConfig(path string) (config *schema.Config, errors []error) {
	errors = nil

	files, err := filepath.Glob(filepath.Join(path, "*.dogo"))
	if err != nil {
		addError(&errors, "", err.Error())
		return
	}

	if len(files) == 0 {
		addError(&errors, "", "Could not find any *.dogo files in the current directory")
		return
	}

	// parse config files into HCL structures
	configFiles := make(map[string]map[string]interface{})
	for _, file := range files {
		fileData, err := ioutil.ReadFile(file)
		if err != nil {
			addError(&errors, file, "Could not read %v. Message: %v", err.Error())
			return
		}

		m := make(map[string]interface{})
		err = hcl.Decode(&m, string(fileData))
		if err != nil {
			addError(&errors, file, "Could not parse config file. Message: %v", err.Error())
			return
		}
		configFiles[file] = m
	}

	// create config
	config = &schema.Config{
		Environments:   make(map[string]*schema.Environment),
		Packages:       make(map[string]*schema.Package),
		TemplateSource: newTemplateSource(),
	}

	// prepare constructors
	moduleConstructor := make(map[string]*constructor.Constructor)
	resourceConstructor := make(map[string]*constructor.Constructor)
	resourceGroupConstructor := make(map[string]*constructor.Constructor)
	tunnelConstructor := constructor.New(&schema.Tunnel{}, config.TemplateSource.NewTemplate)
	commandConstructor := constructor.New(&schema.Command{}, config.TemplateSource.NewTemplate)
	for _, manager := range registry.ModuleManagers {
		if manager.ModulePrototype != nil {
			moduleConstructor[manager.Name] = constructor.New(manager.ModulePrototype, config.TemplateSource.NewTemplate)
		}
	}

	for _, manager := range registry.ResourceManagers {
		resourceConstructor[manager.Name] = constructor.New(manager.ResourcePrototype, config.TemplateSource.NewTemplate)
		resourceGroupConstructor[manager.Name] = constructor.New(manager.GroupPrototype, config.TemplateSource.NewTemplate)
	}

	commandPrototypes := make(map[string]map[string]interface{}) // commandName => args for command.

	// parse packages
	parsePackages(&errors, config, configFiles, tunnelConstructor, commandConstructor, commandPrototypes)

	// parse environments
	parseEnvironments(&errors, config, configFiles, moduleConstructor, resourceConstructor, resourceGroupConstructor, commandConstructor, commandPrototypes)

	return
}

func setTemplateGlobals(config *schema.Config, env *schema.Environment) {
	for k, v := range env.Vars {
		config.TemplateSource.AddGlobal(k, v)
	}

	resourcesByPackage := make(map[string][]interface{})
	for p, resArr := range env.ResourcesByPackage {
		arr, found := resourcesByPackage[p]
		if !found {
			arr = make([]interface{}, 0)
		}
		for _, res := range resArr {
			arr = append(arr, res.Data)
		}
		resourcesByPackage[p] = arr
	}
	config.TemplateSource.AddGlobal("resourcesbypackage", resourcesByPackage)

	resources := make([]interface{}, 0, len(env.Resources))
	for _, res := range env.Resources {
		resources = append(resources, res.Data)
	}
	config.TemplateSource.AddGlobal("resources", resources)

	for _, res := range env.Resources {
		expandResourceTemplates(res, config)
	}
}

func parsePackages(errors *[]error, config *schema.Config, configFiles map[string]map[string]interface{}, tunnelConstructor, commandConstructor *constructor.Constructor, commandPrototype map[string]map[string]interface{}) {
	tunnelNames := make(map[string]bool)

	for filename, file := range configFiles {
		for name, v := range file {
			location := filename
			if name == "package" {
				v2, ok := v.([]map[string]interface{})

				if !ok {
					addError(errors, location, "Invalid package in file")
					continue
				}

				for _, v3 := range v2 {
					for packageName, packageDefinition := range v3 {
						location = filename + " -> package." + packageName

						v4, ok := packageDefinition.([]map[string]interface{})
						if !ok {
							addError(errors, filename, "Invalid package definition of '%v'", packageName)
							continue
						}

						pack, found := config.Packages[packageName]
						if !found {
							pack = &schema.Package{
								Name:     packageName,
								Modules:  make([]*schema.PackageModule, 0, 0),
								Tunnels:  make(map[string]*schema.Tunnel),
								Commands: make(map[string]*schema.Command),
							}
							config.Packages[packageName] = pack
						}

						for _, v5 := range v4 {
							for a, v6 := range v5 {
								location = filename + " -> package." + packageName + "." + a

								v7, ok := v6.([]map[string]interface{})
								if !ok {
									addError(errors, location, "Invalid definition")
									continue
								}
								for _, v8 := range v7 {
									switch a {
									case "tunnel":
										for tunnelName, v9 := range v8 {
											location = filename + " -> package." + packageName + "." + a + "." + tunnelName
											args, ok := v9.([]map[string]interface{})
											if !ok {
												addError(errors, location, "Invalid definition")
												continue
											}
											it, errs := tunnelConstructor.Construct("", args, nil)
											addErrors(errors, location, errs)
											if tunnel, ok := it.(*schema.Tunnel); ok {
												if _, found := tunnelNames[tunnelName]; found {
													addError(errors, location, "The tunnel name '%v' is already in use", tunnelName)
												}
												tunnelNames[tunnelName] = true
												pack.Tunnels[tunnelName] = tunnel
											}
										}
										break
									case "command":
										for commandName, v9 := range v8 {
											location = filename + " -> package." + packageName + "." + a + "." + commandName
											args, ok := v9.([]map[string]interface{})
											if !ok {
												addError(errors, location, "Invalid definition")
												continue
											}
											it, errs := commandConstructor.Construct("", args, nil)
											addErrors(errors, location, errs)
											if command, ok := it.(*schema.Command); ok {
												if _, found := commandPrototype[commandName]; found {
													addError(errors, location, "The command name '%v' is already in use", commandName)
												}
												if len(command.Tunnels) > 0 && !command.Local {
													addError(errors, location, "The command name '%v' has tunnels, but is not marked as a local command.", commandName)
												}
												commandPrototype[commandName] = flatten(args)
												pack.Commands[commandName] = command
											}
										}
										break
									default:

										// validate module name is registrered.
										if _, found := registry.ModuleManagers[a]; !found {
											addError(errors, location, "Unknown module: %v", a)
										}

										// create the package module
										m := &schema.PackageModule{
											OriginalLocation: location,
											ModuleName:       a,
											Config:           make(map[string]interface{}),
										}
										for k, v := range v8 {
											m.Config[k] = v
										}

										pack.Modules = append(pack.Modules, m)

										break
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// check that tunnel names used in commands are valid
	for _, pack := range config.Packages {
		for commandName, cmd := range pack.Commands {
			for _, name := range cmd.Tunnels {
				if findTunnel(config, name) == nil {
					addError(errors, "command: "+commandName, "There is no tunnel with the name '%v'", name)
				}
			}
		}
	}

	return
}

func parseEnvironments(errors *[]error, config *schema.Config, configFiles map[string]map[string]interface{}, moduleConstructor, resourceConstructor map[string]*constructor.Constructor, resourceGroupConstructor map[string]*constructor.Constructor, commandConstructor *constructor.Constructor, commandPrototype map[string]map[string]interface{}) {
	for filename, file := range configFiles {
		for name, v := range file {
			location := filename
			if name == "environment" {
				v2, ok := v.([]map[string]interface{})
				if !ok {
					addError(errors, location, "Invalid package in file")
					continue
				}

				for _, v3 := range v2 {
					for environmentName, environmentDefinition := range v3 {
						location = filename + " -> environment." + environmentName

						env, found := config.Environments[environmentName]
						if !found {
							env = &schema.Environment{
								Vars:            make(map[string]interface{}),
								Name:            environmentName,
								Resources:       make(map[string]*schema.Resource),
								DeploymentHooks: make([]*schema.DeploymentHook, 0),
							}
							config.Environments[environmentName] = env
						}

						v4, ok := environmentDefinition.([]map[string]interface{})
						if !ok {
							addError(errors, location, "Invalid environment definition of '%v'", environmentName)
							continue
						}

						for _, v5 := range v4 {
							for providerName, providerConfig := range v5 {
								location = filename + " -> environment." + environmentName + "." + providerName

								v6, ok := providerConfig.([]map[string]interface{})
								if !ok {
									if boolValue, ok := providerConfig.(bool); ok {
										env.Vars[providerName] = boolValue
									} else if intValue, ok := providerConfig.(int); ok {
										env.Vars[providerName] = intValue
									} else if stringValue, ok := providerConfig.(string); ok {
										env.Vars[providerName] = stringValue
									} else {
										addError(errors, location, "Invalid resource provider definition of '%v' (%T)", providerName, providerConfig)
									}
									continue
								}

								env.ManagerGroups = make(map[string][]interface{})
								if resourceManager, found := registry.ResourceManagers[providerName]; found {
									for _, g := range v6 {
										group, errs := resourceGroupConstructor[providerName].Construct(location+".", []map[string]interface{}{g}, nil)
										addErrors(errors, location, errs)

										//arr, found :=
										env.ManagerGroups[resourceManager.Name] = append(env.ManagerGroups[resourceManager.Name], group) //= append(env.ManagerGroups, group)

										for name, conf := range g {
											if name == "server" {
												location = filename + " -> environment." + environmentName + "." + providerName + "." + name
												v7, ok := conf.([]map[string]interface{})
												if !ok {
													addError(errors, location, "Invalid server definition of '%v'", providerName)
													continue
												}

												for _, v8 := range v7 {
													for serverName, conf := range v8 {
														location = filename + " -> environment." + environmentName + "." + providerName + "." + name + "." + serverName
														v9, ok := conf.([]map[string]interface{})
														if !ok {
															addError(errors, location, "Invalid resource provider definition of '%v'", providerName)
															continue
														}
														v9flat := flatten(v9)

														// how many resources to create
														count := 1
														hasCount := false
														if v, ok := v9flat["count"]; ok {
															delete(v9flat, "count")
															if ctr, ok := v.(int); ok && ctr > 0 && ctr < 10000 {
																hasCount = true
																count = ctr
															}
														}

														// create resources
														for i := 1; i <= count; i++ {
															instanceName := serverName
															if count > 1 || hasCount {
																instanceName = fmt.Sprintf("%v_%v", serverName, i)
															}

															// check that the name is unique
															if _, found := env.Resources[instanceName]; found {
																addError(errors, location, "the environment already has a resource with the name: %v", instanceName)
																continue
															}

															// build data map
															data := make(map[string]interface{})
															modules := make(map[string]interface{})
															data["modules"] = modules
															data["name"] = instanceName
															data["instanceid"] = i
															for k, v := range v9flat {
																if _, found := registry.ModuleManagers[k]; found {
																	continue // skip module extra config
																}
																data[k] = v
															}

															// build modules list for every module
															for _, m := range registry.ModuleManagers {
																if m.ModulePrototype != nil {
																	modules[m.Name] = reflect.MakeSlice(reflect.SliceOf(reflect.TypeOf(m.ModulePrototype)), 0, 0).Interface()
																}
															}

															// build modules from all used packages
															usedPackages := make(map[string]bool)
															if packages, found := v9flat["package"]; found {
																packV, ok := packages.([]map[string]interface{})
																if !ok {
																	addError(errors, location, "Invalid package definition", providerName)
																	continue
																}
																for _, a := range packV {
																	for packageName, extraPackageArgs := range a {
																		p, found := config.Packages[packageName]
																		if !found {
																			addError(errors, location, "Unknown package: %v", packageName)
																			continue
																		}

																		usedPackages[packageName] = true

																		// create module each module in package
																		for _, mod := range p.Modules {
																			args := make([]map[string]interface{}, 0, 0)
																			args = append(args, mod.Config)

																			// grab extra config for module defined inline
																			if extra, found := v9flat[mod.ModuleName]; found {
																				v8, ok := extra.([]map[string]interface{})
																				if !ok {
																					addError(errors, location, "Invalid extra config for module '%v'", mod.ModuleName)
																					continue
																				}

																				for _, m := range v8 {
																					args = append(args, m)
																				}
																			}

																			// varmap
																			vars := make(map[string]interface{})
																			vars["self"] = data

																			// grab extra config defined where the package is added to a server
																			if extraPackageArgsT, ok := extraPackageArgs.([]map[string]interface{}); ok {
																				args = append(args, extraPackageArgsT...)
																				for _, m := range extraPackageArgsT {
																					for k, v := range m {
																						vars[k] = v
																					}
																				}
																			}

																			// create module
																			if constructor, ok := moduleConstructor[mod.ModuleName]; ok {
																				m, errs := constructor.Construct(mod.ModuleName+".", args, vars)
																				addErrors(errors, location, errs)
																				if m != nil {
																					arr := modules[mod.ModuleName]
																					arrValue := reflect.ValueOf(arr)
																					modules[mod.ModuleName] = reflect.Append(arrValue, reflect.ValueOf(m)).Interface()
																				}
																			}
																		}
																	}
																}
															}

															// construct resource.
															vars := make(map[string]interface{})
															vars["self"] = data
															resource, errs := resourceConstructor[providerName].Construct("", []map[string]interface{}{data}, vars)
															addErrors(errors, location, errs)

															// add the resource
															if resource != nil {
																env.Resources[instanceName] = &schema.Resource{
																	Name:         instanceName,
																	Data:         data,
																	Manager:      resourceManager,
																	ManagerGroup: group,
																	Packages:     usedPackages,
																	Modules:      modules,
																	Resource:     resource,
																}
															}
														}
													}
												}
											} else {
												//addError(errors, location, "Unknown element '%v'", name)
											}

											continue
										}
									}
								} else if providerName == "before_deployment" || providerName == "after_deployment" {
									location = filename + " -> environment." + environmentName + "." + providerName

									v6, ok := providerConfig.([]map[string]interface{})
									if !ok {
										addError(errors, location, "Invalid %v definition", providerName)
										continue
									}

									for _, v7 := range v6 {
										for commandName, commandArgs := range v7 {
											var pack *schema.Package
											for _, p := range config.Packages {
												if _, found := p.Commands[commandName]; found {
													pack = p
													break
												}
											}
											if pack == nil {
												addError(errors, location, "Unknown command '%v' used in %v. (can't find it in any package)", commandName, providerName)
												continue
											}

											extraArgs := make(map[string]interface{})
											if v8, ok := commandArgs.([]map[string]interface{}); ok {
												for k, v := range flatten(v8) {
													extraArgs[k] = v
												}
											}

											it, errs := commandConstructor.Construct("", []map[string]interface{}{
												commandPrototype[commandName],
												extraArgs,
											}, nil)
											addErrors(errors, location, errs)
											if command, ok := it.(*schema.Command); ok {
												if len(command.Tunnels) > 0 && !command.Local {
													addError(errors, location, "The command name '%v' has tunnels, but is not marked as a local command.", commandName)
												}
												env.DeploymentHooks = append(env.DeploymentHooks, &schema.DeploymentHook{
													Command:             command,
													CommandName:         commandName,
													CommandPackage:      pack.Name,
													RunBeforeDeployment: providerName == "before_deployment",
													RunAfterDeployment:  providerName == "after_deployment",
												})
											}
										}
									}
								} else {
									addError(errors, location, "Unknown element '%v'", providerName)
								}
							}
						}

						// build ResourcesByPackage
						env.ResourcesByPackage = make(map[string][]*schema.Resource)
						for _, key := range sortKeysRes(env.Resources) {
							res := env.Resources[key]
							for name := range res.Packages {
								arr, found := env.ResourcesByPackage[name]
								if !found {
									arr = make([]*schema.Resource, 0, 10)
								}
								env.ResourcesByPackage[name] = append(arr, res)
							}
						}
					}
				}
			}
		}
	}

	return
}

func sortKeysRes(m map[string]*schema.Resource) []string {
	arr := make([]string, 0, len(m))
	for k := range m {
		arr = append(arr, k)
	}
	sort.Strings(arr)
	return arr
}

func flatten(input []map[string]interface{}) map[string]interface{} {
	output := make(map[string]interface{})
	for _, m := range input {
		for k, v := range m {
			output[k] = v
		}
	}

	return output
}

func findTunnel(config *schema.Config, tunnelName string) *schema.Tunnel {
	for _, p := range config.Packages {
		if tunnel, found := p.Tunnels[tunnelName]; found {
			return tunnel
		}
	}
	return nil
}
