package configloader

import (
	"fmt"
	"reflect"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

// decode decodes every layer into cfg, rejecting keys that match no field unless allowUnknown is set.
func (l *loader) decode(cfg any, allowUnknown bool) error {
	hooks := viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		scrubbed(mapstructure.StringToTimeDurationHookFunc()),
		scrubbed(mapstructure.TextUnmarshallerHookFunc()),
		// After the text hook, so a type such as net.IP, a []byte that decodes itself from text, isn't split on commas.
		mapstructure.StringToWeakSliceHookFunc(","),
	))

	if allowUnknown {
		return l.v.Unmarshal(cfg, hooks)
	}

	return l.v.UnmarshalExact(cfg, hooks)
}

// scrubbed wraps hook so that on failure it names only the target type: parse errors often echo their input, in
// whole or in part, and config values include secrets.
func scrubbed(hook mapstructure.DecodeHookFunc) mapstructure.DecodeHookFuncValue {
	return func(from, to reflect.Value) (any, error) {
		out, err := mapstructure.DecodeHookExec(hook, from, to)
		if err != nil {
			return nil, fmt.Errorf("cannot parse value as %s", to.Type())
		}

		return out, nil
	}
}
