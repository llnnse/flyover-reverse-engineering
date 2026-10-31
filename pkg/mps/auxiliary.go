package mps

import (
	"errors"
	fmt "fmt"
	"regexp"

	"github.com/golang/protobuf/proto"
)

func extractModernResourcesURL(unrecognized []byte) string {
	b := proto.NewBuffer(unrecognized)
	for {
		wire, err := b.DecodeVarint()
		if err != nil {
			break
		}
		fieldNum := wire >> 3
		wireType := wire & 7
		if wireType == proto.WireBytes {
			bytesVal, err := b.DecodeRawBytes(false)
			if err != nil {
				break
			}
			if fieldNum == 92 {
				b2 := proto.NewBuffer(bytesVal)
				for {
					w2, err := b2.DecodeVarint()
					if err != nil {
						break
					}
					fn2 := w2 >> 3
					wt2 := w2 & 7
					if wt2 == proto.WireBytes {
						bytesVal2, err := b2.DecodeRawBytes(false)
						if err != nil {
							break
						}
						if fn2 == 2 {
							b3 := proto.NewBuffer(bytesVal2)
							for {
								w3, err := b3.DecodeVarint()
								if err != nil {
									break
								}
								fn3 := w3 >> 3
								wt3 := w3 & 7
								if wt3 == proto.WireBytes {
									s, err := b3.DecodeStringBytes()
									if err != nil {
										break
									}
									if fn3 == 1 {
										return s
									}
								} else {
									b3.DecodeVarint()
								}
							}
						}
					} else {
						b2.DecodeVarint()
					}
				}
			}
		} else if wireType == proto.WireVarint {
			b.DecodeVarint()
		} else if wireType == proto.WireFixed32 {
			b.DecodeFixed32()
		} else if wireType == proto.WireFixed64 {
			b.DecodeFixed64()
		}
	}
	return ""
}

func (rm ResourceManifest) URLPrefixFromStyleID(styleID ResourceManifest_StyleConfig_StyleID) (pfx string, err error) {
	for _, v := range rm.StyleConfig {
		if v.StyleId == styleID {
			pfx = v.UrlPrefix_1
			break
		}
	}
	if pfx == "" {
		err = errors.New(fmt.Sprint("no url prefix found for style", styleID))
	}
	return
}

func (rm ResourceManifest) CacheFileNameFromRegexp(regexp *regexp.Regexp) (fn string, err error) {
	for _, cf := range rm.CacheFile {
		if regexp.MatchString(cf.FileName) {
			fn = cf.FileName
			break
		}
	}
	if fn == "" {
		for _, cf := range rm.CacheFile_2 {
			if regexp.MatchString(cf) {
				fn = cf
				break
			}
		}
	}
	if fn == "" {
		err = errors.New(fmt.Sprint("no cache file for regexp", regexp.String()))
	}
	return
}
