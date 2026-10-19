package trace

import (
	"go.uber.org/fx"
)

// Module provides the app's oteltrace.TracerProvider, built by New. It needs telemetry.Module and a *slog.Logger in
// the app.
var Module = fx.Module("telemetry/trace", fx.Provide(New))
