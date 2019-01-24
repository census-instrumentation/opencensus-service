package compression

var (
	supportedCompressionTypes = map[string]bool{
		"gzip": true,
	}
)

func IsSupportedCompressionType(compression string) bool {
	if supported, ok := supportedCompressionTypes[compression]; ok && supported {
		return true
	}
	return false
}
