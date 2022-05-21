
<!--
order: 7
-->

# Parameters

The market module contains the following parameters:

| Key                      | Type         | Example                |
|--------------------------|--------------|------------------------|
| voteperiod               | string (int) | "5"                    |
| votethreshold            | string (dec) | "0.500000000000000000" |
| rewardband               | string (dec) | "0.020000000000000000" |
| rewarddistributionwindow | string (int) | "5256000"              |
| whitelist                | []DenomList  | [{"name": "ukrw", tobin_tax": "0.002000000000000000"}] |
| slashfraction            | string (dec) | "0.001000000000000000" |
| slashwindow              | string (int) | "100800"               |
| minvalidperwindow        | string (int) | "0.050000000000000000" |