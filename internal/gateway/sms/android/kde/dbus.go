package kde

const (
	serviceName = "org.kde.kdeconnect"
	servicePath = "/modules/kdeconnect"
)

var signalNames = [...]string{
	"stateChanged",
	"reachableChanged",
	"trustedChanged",
	"nameChanged",
	"typeChanged",
	"pluginsChanged",
}

var propNames = [...]string{
	"", // signalName 'stateChanged' has no prop
	"isReachable",
	"isTrusted",
	"name",
	"type",
	"supportedPlugins",
}

var fieldNames = [...]string{
	"", // signalName 'stateChanged' has no field
	"Reachable",
	"Trusted",
	"Name",
	"Type",
	"Plugins",
}

var fieldTypes = [...]string{
	"",
	"bool",
	"bool",
	"string",
	"string",
	"set",
}
