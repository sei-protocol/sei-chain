package test

import "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"

// migrationPuts is the canonical input written to the migration-test fixture. It mirrors what real
// callers do: a sequence of Puts, some of which include secondary keys. Three primaries near the
// end carry secondaries that exercise every KeyKind path:
//
//   - "kindStandalone-primary" is a 0-secondary Put (covered by every other entry too, but called
//     out by name for readability).
//   - "kindPrimary-with-one-secondary" carries exactly one secondary, exercising
//     KeyKindPrimary + KeyKindFinalSecondary.
//   - "kindPrimary-with-three-secondaries" carries three secondaries (a mix of strict sub-range
//     and alias-the-whole-value), exercising KeyKindPrimary + 2× KeyKindSecondary +
//     KeyKindFinalSecondary.
//
// Cross-version migration verifies that every primary AND every secondary survives the round
// trip through whatever the current on-disk format happens to be.
var migrationPuts = func() []*types.PutRequest {
	out := make([]*types.PutRequest, 0, len(migrationData)+3)
	for key, value := range migrationData {
		out = append(out, &types.PutRequest{Key: []byte(key), Value: []byte(value)})
	}

	out = append(out,
		&types.PutRequest{
			Key:   []byte("kindStandalone-primary"),
			Value: []byte("standalone"),
		},
		&types.PutRequest{
			Key:   []byte("kindPrimary-with-one-secondary"),
			Value: []byte("hello world"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("kindFinal-only-secondary"), Offset: 0, Length: 5}, // "hello"
			},
		},
		&types.PutRequest{
			Key:   []byte("kindPrimary-with-three-secondaries"),
			Value: []byte("the quick brown fox jumps over the lazy dog"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("kindMid-quick"), Offset: 4, Length: 5},                                 // "quick"
				{Key: []byte("kindMid-brown"), Offset: 10, Length: 5},                                // "brown"
				{Key: []byte("kindFinal-alias-whole"), Offset: 0, Length: 43 /* len of the value */}, // alias the whole value
			},
		},
	)

	return out
}()

// expectedMigrationKVs flattens migrationPuts into a single map from key bytes -> expected bytes.
// Every primary key maps to its full value; every secondary key maps to the sub-range of its
// parent's value bytes that it points at.
var expectedMigrationKVs = func() map[string]string {
	out := make(map[string]string, len(migrationPuts))
	for _, p := range migrationPuts {
		out[string(p.Key)] = string(p.Value)
		for _, sk := range p.SecondaryKeys {
			out[string(sk.Key)] = string(p.Value[sk.Offset : sk.Offset+sk.Length])
		}
	}
	return out
}()

// migrationData is the original key->value fixture from v3 and earlier. Newer fixtures extend it
// via migrationPuts above; keeping migrationData as a standalone map preserves the historical
// payload (so generated v3 data remains byte-for-byte identical were one to regenerate it under
// the old code).
var migrationData = map[string]string{
	"S7MOxfceWW":          "oSNhtpEtRb48ntgPkhL",
	"uQxQ25apaahwztuOzNi": "Tn2MgaTP5B",
	"cdlFwQ3izP6gddTWg":   "lrB2OPxXpvA9GEr",
	"BUHqRs6XNnk":         "XiM14PxeApDwgCwoWl",
	"iMV7t0BLFhp8WDt3z":   "AtkhY6eBDwJjPC9Yq0",
	"9v3kYNhyWqpbXKjB":    "fXVDjf4H3LAPZo",
	"fZLvo7jDSSlWP":       "uhI9oNwGZvOR",
	"3pkkNwZmFgO1":        "p2EakPC1qFy1Ln7X1gy",
	"k30CXpbPH7N":         "CJPo06kCod8H5nl",
	"bK6ShP3Ji9FN":        "dCXgS4SlWnmo",
	"lYtAmE5Oe0wYeLTr":    "26b4nHzUbnFbragc6D",
	"chzmznu42ET4i":       "bUHbWNpRnJFmR5zdgMY",
	"QWGu2AnfcifYECejE":   "26FYmPjkYs51nh98",
	"4aEyphJuc5":          "6xevs3LFY58gxg",
	"aQ0Y9rb1UisYU03FW":   "ontvK6EElNxUt",
	"kYCtV1TdwjO":         "qQZMRlvQ4MJRRST",
	"U2E9LMOhu0uY1DL":     "5P1OmVO3hI1PI",
	"dysi8hDsKj8FF":       "w2Fkpvl9PAI",
	"LcUMjv2DlnS":         "6vZh6B840MN8W8Edx",
	"XxAUWO6zyJ":          "blcXwtWmVB8Xkzv",
	"lWQkLUVEFMS":         "K2xRiBNQ5MNb75d3B",
	"n64zlB9gKtk":         "Arky8MofGkvEhFNc",
	"ZEeVJZTz6372d":       "BmAwd2EvHw",
	"6B1wwUMjTF":          "428u9CE6zZlQoWG",
	"sg7u1aDylz":          "w4XuLp12Gg6pWll",
	"ivHrCBthr8qu":        "i1BYGFSfM3P",
	"f8y4xuM57qFQg":       "haThtIFGmQ2a1",
	"7Lw3q58svTi4SEAFw":   "QQZ9cqPEq2VVR",
	"NRrxErIRM4":          "MuP0gvMHSbk51W93N",
	"zmNLDGiOsX0zzLxgqx":  "rIea0vLsQnLpL",
	"R12vsDgE9vHSh":       "ofNCxSlZx44UPkG8C04",
	"UFjhyw212E1HB":       "FlWDrgzeshrq",
	"ue2g7bcwq1xS":        "fbJrgwABL86Kh",
	"jrDRPJ1uXPLeJxwbDdp": "4TGH4FzHWSUn5oc",
	"j8GIOZUCpcotvNs":     "D4MBDXATSN",
	"3UwjwlxbofoH":        "l1R6uK4eCQ",
	"dNmMpVGPQpUkcUE":     "vaPjmDx1lP",
	"2nk7LDEAIiP17i":      "3G5RAf58WUmqTEQed",
	"LMCzFVEVHL8yozVw3X":  "pMyKVDIUyz",
	"mvyYTJEO2cJ6oY3L4U":  "M5s0cyA2UJ3jstDz",
	"Bx0ARO4F4BSg":        "NtCNQZAEuJizQhXXL",
	"6x45pVeBPckE9Rbb":    "CTFHvtahyIn0CAN",
	"4Upqz2PKSR1":         "6PpFUoLqEtg5QLPf7Q",
	"sJtKBhkqXJ8QjPab":    "KNhNwNybSgp0hjsayh",
	"UxtCua2isEaZAuCEM1":  "CV1D4By3PkfctVA8pEA",
	"kkVYsbOBrIhrm":       "UXtbSmjYPR",
	"MfA1l81VnHH":         "qECowRfgz0",
	"xFSCCXEBQfVB":        "jxRBNQOMpHErksJu4",
	"EvJlXug4Lj":          "xa6IUSXbcqxdo",
	"KC9ljchlpJGC":        "QH2dqRdzH7Vr",
	"C8kiIIMWffu5UH":      "ZGzgRuGu55bFY",
	"qB8FM7KKVM192bW7c":   "R8AEX7ZSVc5Kku",
	"2WvlDWvByFAjHGO5":    "ToPJqT4cHpuK7j7oHs",
	"Y21Q4luB2YR9tkH":     "2H41w79yXlFcxg",
	"EdLROPjF0lrQR5Y":     "VpmOg5d6Ya",
	"9OIQkcyEZ4V0hgJT":    "3kwfJ9pzGeB67Y",
	"eHhgOVn7XZBvp":       "3W9GuwG3XH0",
	"7PTApk1JZnegET":      "0K4RIpQbBU",
	"zO3XDUKdmFWhzwL8":    "zol4hrMcjKh4wXBW0X4",
	"anEZPbHRLgbK8ab8k":   "TuVWcQMIUC3w",
	"8zjsG3w3mP":          "Lus1iBWnndJca1BGPw",
	"i1RqPkH2XKRj4wS":     "UaaoCv0nA6DuXQ",
	"35RKf4sd9a":          "GHinZXfMWGfZqfrEUj",
	"sX3VM3pdWuTN":        "qu1IYzyZXWSrRt0Q7",
	"DQXDdUJvMijK":        "KJ9lMw28tR3i5CzSOe",
	"8G9r4r7hKZs":         "zryjRgkY3B9",
	"Ge55N78jIGzl4kyWAQ":  "IToFVMqwa2woQfsh",
	"4KcWZuzvlSMI":        "cbBr5XwaDgyduz7lF",
	"iHCadisZ2d4Lhh":      "RqsHSDNJbX",
	"KnHZhDP4EezmNcH6waF": "5qDf9Tg08OHwOyrbV",
	"2VFfY7yWW5cEs":       "vxwc3n4trq3D",
	"Cl74jcT7McogOuI":     "zEpiTYqMnM4AEpQecs",
	"C3ZqqO4cenvQhUXr5":   "ro7MlUTDJt3yCG4I9x",
	"J0iTmnA2jc0g":        "oImOAez9d2M3LodO",
	"Xg9t7f0x9F":          "4kD25VKJGYTJXNScjKI",
	"2qIhPhR0tqr0sf9n":    "67hj2DdNr8",
	"c2D85oqCiSFv344vw":   "24ptxcYqnwu",
	"nSlaWA77r6Dqbl3Lyv":  "KcMnVtYPwgcqT",
	"EpfdcYJauGI":         "XzBcPMUZyryB",
	"j0FvUY2kdcFehwSFTPU": "MqA1KDBYG53K",
	"MHwGBaYMRtPVX":       "cTqqONfvuSAtt",
	"x5yJoUs8wOwkiiiao":   "syZQNyr47tVH4",
	"K3LPe7EsYmzmZfmJSr":  "VT0tSNW17vJ",
	"snbz01TFonWpok1WQJQ": "dkLkKFlbNsRhgCZGsp",
	"KYL5i7mIx6I95dO0":    "74ndgZk9ymMxhn0spv",
	"b2yGXFlpHJuQwpCaa":   "ZuvhlCcIRKcdn",
	"fycSvFVXdL7":         "Al7tASqhEtUxwv8O8",
	"UY9YfW75SzDqCPy":     "Mz9q5TUxPfkh",
	"OGfnB7QR4eQaatXwP":   "t3zE0G6XVVG",
	"2S3X8sDLwDNk":        "kDUv68Hm807FEDCj",
	"zMJPfHe0Td4m5JLD":    "4XUTqdsnQPtI2Bk",
	"4plod7WQcLypxeJ24B4": "flw6IHhUi8NmZ",
	"UMlCE2OHHYREl":       "QOaCQaRS67dCW6",
	"nz7DN3LHVWsjEPVD":    "4tndorV1Yltoz",
	"dUVvq2B95CkIOHn":     "QqgioH4rseg",
	"ypMpA354f9xP":        "CuskocQHlFcYtG",
	"TejKR8aotSlTBW78Mt":  "7dvQROKGAjCFfEHmHT",
	"hZ9XON4x4WivPJ3":     "TuVgbSDFtna5dv",
	"Z3IErKLZrStej27":     "JLZ1yjpuYQXFRsG",
	"azDFe3GvhnR":         "fYw79uPHmN",
}
