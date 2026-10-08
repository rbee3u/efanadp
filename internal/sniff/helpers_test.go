package sniff

import "crypto/aes"

func sniffShadowsocksTarget(config ShadowsocksConfig, data []byte) (string, shadowsocksSniffState) {
	sniffer := NewShadowsocksTCPSniffer([]ShadowsocksConfig{config}, config.Server, config.Port)
	if sniffer == nil {
		return "", sniffFailed
	}
	return sniffer.sniff(data)
}

func sniffShadowsocksUDPTarget(config ShadowsocksConfig, data []byte) (string, bool) {
	sniffer := NewShadowsocksUDPSniffer([]ShadowsocksConfig{config}, config.Server, config.Port)
	return sniffer.Sniff(data)
}

func sniffShadowsocks2022UDPTargetWithKey(
	keys shadowsocks2022Keys,
	spec shadowsocksCipherSpec,
	data []byte,
) (string, bool) {
	block, err := aes.NewCipher(keys.blockKey)
	if err != nil {
		return "", false
	}
	sniffer := &ShadowsocksUDPSniffer{spec: spec, key: keys.userKey, eihLen: keys.eihLen, block: block}
	return sniffer.sniff2022(data)
}

func deriveShadowsocksSubkey(password string, salt []byte, keySize int) ([]byte, error) {
	masterKey := evpBytesToKey([]byte(password), keySize)
	return deriveShadowsocksSubkeyFromMasterKey(masterKey, salt, keySize)
}
