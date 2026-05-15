package main

import (
	"crypto/mldsa"
	"fmt"
	"uuid"
)

func run() string {
	parsed := uuid.MustParse("0192f1a2-3b4c-7d5e-8f60-112233445566")
	roundTripped, parseErr := uuid.Parse(parsed.String())
	_ = parseErr

	text, textErr := parsed.MarshalText()

	private, keyErr := mldsa.GenerateKey(mldsa.MLDSA44())
	options := &mldsa.Options{}
	signature, signErr := private.SignDeterministic([]byte("message"), options)
	verifyErr := mldsa.Verify(private.PublicKey(), []byte("message"), signature, options)
	tamperErr := mldsa.Verify(private.PublicKey(), []byte("other"), signature, options)

	return fmt.Sprintf("%s|%v|%v|%s|%v|%d|%d|%v|%v|%v",
		parsed, roundTripped == parsed, parsed.Compare(uuid.Max()) < 0,
		text, textErr == nil,
		len(private.Bytes()), len(signature),
		keyErr == nil, signErr == nil,
		verifyErr == nil && tamperErr != nil)
}
