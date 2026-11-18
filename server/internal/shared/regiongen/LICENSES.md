# Code from elsewhere

| File | Source | Author | Licence | Changes |
| --- | --- | --- | --- | --- |
| `noise.go` | [ojrac/opensimplex-go](https://github.com/ojrac/opensimplex-go) v1.0.2, commit 67ecc326c2dd290b2e1a579e1ffd3f4818cc73f8: `New` and `Eval2` | Kurt Spencer's OpenSimplex, ported to Go by Owen Raccuglia | The Unlicense: public domain | 2D only; every product rounded on its own (`float64(a*b)`), so arm64 can't fuse it, which leaves its values on amd64 bit for bit |
