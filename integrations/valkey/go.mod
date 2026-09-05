module github.com/Intent-IQ/identity-go/integrations/valkey

go 1.25.0

require (
	github.com/Intent-IQ/identity-go v0.0.0
	github.com/alicebob/miniredis/v2 v2.38.0
	github.com/go-ozzo/ozzo-validation/v4 v4.3.0
	github.com/valkey-io/valkey-go v1.0.76
)

require (
	github.com/asaskevich/govalidator v0.0.0-20200108200545-475eaeb16496 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/coocood/freecache v1.2.1 // indirect
	github.com/prebid/openrtb/v20 v20.3.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	golang.org/x/sys v0.43.0 // indirect
)

replace github.com/Intent-IQ/identity-go => ../..
