module github.com/Intent-IQ/identity-go/example/basic

go 1.25.0

require (
	github.com/Intent-IQ/identity-go v0.0.0
	github.com/Intent-IQ/identity-go/integrations/valkey v0.0.0
	github.com/prebid/openrtb/v20 v20.3.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/asaskevich/govalidator v0.0.0-20200108200545-475eaeb16496 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/coocood/freecache v1.2.1 // indirect
	github.com/go-ozzo/ozzo-validation/v4 v4.3.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/valkey-io/valkey-go v1.0.76 // indirect
	golang.org/x/sys v0.43.0 // indirect
)

replace github.com/Intent-IQ/identity-go => ../..

replace github.com/Intent-IQ/identity-go/integrations/valkey => ../../integrations/valkey
