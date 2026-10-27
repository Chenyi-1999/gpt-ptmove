Optional verified cache for BDInfo, BDInfoDataSubstractor and liblzfse.so.

The Docker build downloads missing helpers from the pinned upstream commit.
For slow builder networks, prefetch those public files and copy them here before
building. fetch_native_assets.py verifies the exact Git blob identities before
using any cached helper. Generated binary files are excluded from Git.
