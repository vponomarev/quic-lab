package mobile
import "testing"
func TestMDMConfigurationRejectsUnknownSchema(t *testing.T) {
 if ValidateMDMConfiguration("{\"schema\":2}")==nil {t.Fatal("unknown schema")}
}
