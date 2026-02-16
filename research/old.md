# PSSH

## can we use PSSH for key IDs?

no, https://hulu.com PSSH includes L3 and L1 keys IDs

## test 1

1. check MPD for `default_KID`
2. check MP4 for `default_KID`
3. check MP4 for content ID

https://rakuten.tv needs content ID, and its only in MPD

## test 2

1. check MPD for `default_KID`
2. check MPD for content ID
3. check MP4 for `default_KID`

https://ctv.ca needs content ID, and its only in MP4

## test 3

1. check MPD for `default_KID`
2. check MPD for content ID
3. check MP4 for content ID

