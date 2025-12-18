package pointers

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }

func Float64(v float64) *float64 { return &v }
func Int(v int) *int             { return &v }
func String(v string) *string    { return &v }
