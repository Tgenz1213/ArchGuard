package inference

type temperature struct{ value *float64 }

func (t *temperature) SetTemperature(v *float64) { t.value = v }

func (t *temperature) Temperature() *float64 { return t.value }
