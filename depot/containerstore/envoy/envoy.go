package envoy

type DataSource struct {
	InlineString string `yaml:"inline_string"`
}

type TLSCertificate struct {
	CertificateChain DataSource `yaml:"certificate_chain"`
	PrivateKey       DataSource `yaml:"private_key"`
}

type CertificateValidationContext struct {
	TrustedCA            DataSource `yaml:"trusted_ca,omitempty"`
	VerifySubjectAltName []string   `yaml:"verify_subject_alt_name,omitempty"`
}

type CAResource struct {
	Type              string                       `yaml:"@type"`
	Name              string                       `yaml:"name"`
	ValidationContext CertificateValidationContext `yaml:"validation_context"`
}

type SDSCAResource struct {
	VersionInfo string       `yaml:"version_info"`
	Resources   []CAResource `yaml:"resources"`
}

type CertificateResource struct {
	Type           string         `yaml:"@type"`
	Name           string         `yaml:"name"`
	TLSCertificate TLSCertificate `yaml:"tls_certificate"`
}

type SDSCertificateResource struct {
	VersionInfo string                `yaml:"version_info"`
	Resources   []CertificateResource `yaml:"resources"`
}
