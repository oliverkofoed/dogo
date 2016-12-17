package commandtree

var SpinnerCharacters = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

type monitorEventType int

const (
	monitorEventChildAdded  monitorEventType = iota // (parentID, commandId, caption)
	monitorEventStateChange                         // (commandID, newState)
	monitorEventLog                                 // (commandID, logMessage, logIsError, logTime)
	monitorEventResult                              // (commandID, result)
	monitorEventPanic                               // (Caption has the error)
)

type MonitorEvent struct {
	EventType monitorEventType
	CommandID string
	ParentID  string
	State     CommandState
	Caption   string
	LogEntry  *LogEntry
	Result    interface{}
}
