Optional offline Python wheelhouse used only during Docker builds.

Normal source checkout contains this file only and uses PyPI. To build offline,
download the locked runtime packages plus pytest dependencies for the target
Linux/Python architecture into this directory. Docker uses them without network.

Binary wheels are ignored by Git and source releases. Do not copy amd64 wheels
to an ARM NAS; build from the normal checkout or prepare matching ARM wheels.
