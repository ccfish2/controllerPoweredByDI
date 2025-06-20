package helpers

var UserDefinedScope string

func GetScope() (string, error) {
	return "testscope", nil
}

func GetCurrentIntegration() string {
	return "integration_tests"
}
