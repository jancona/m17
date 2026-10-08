# Contributing

Thanks for helping with m17 and m17-gateway. Bug reports, feature requests and pull requests are all welcome.

## Issues first, or with your PR

The most useful thing you can send is a clear description of what should change and why. A good description usually lets me fix the problem even without a patch, and it's what I look at first when a pull request comes in.

For problems, the [problem report form](https://github.com/jancona/m17/issues/new?template=bug_report.yml) asks for all of this. Whether you open an issue or a pull request, please include:

- **What you did, what you expected, and what happened instead.**
- **Your hardware:** the Raspberry Pi model, the modem (CC1200 HAT, SX1255, MMDVM_HS or another MMDVM board) and its firmware version, and the radio on the other end with its firmware.
- **Versions:** `dpkg-query -W m17-gateway`, or the commit you built from.
- **Logs:** set `Level = DEBUG` in the `[Log]` section of `/etc/m17-gateway.ini`, reproduce the problem, and include the output of `journalctl -u m17-gateway`, covering a little before and after.
- **Your configuration:** the relevant sections of `m17-gateway.ini`, minus anything private.
- **Steps to reproduce**, as exactly as you can.
- **Links to the relevant code**, and a sketch of the fix if you have one.

There's no such thing as too much detail. Many problems here depend on hardware I don't have, such as a particular card order or modem revision, so your setup is often the only place the problem can be seen.

## Pull requests

I treat a pull request as a detailed proposal. I may merge it as it is, add commits to it, or write the change myself from your description and close the PR. When your report or code leads to a change, you'll get credit in the commit.

What helps a pull request along:

- **Say how you tested it.** Unit tests, and whether you tried it on air, with which hardware. "Not yet tested on hardware" is fine; just say so.
- **Keep it to one change.** Separate fixes are easier to review and test as separate PRs.
- **Make the logic testable without hardware** where you can. For example, ALSA device selection is a plain function over a list of devices, so its rules have table-driven tests.
- **Run `gofmt`, `go vet` and `go test ./...`.**
- **Update the sample config and docs** when behavior or settings change.

## Protocol changes

This repository implements the [M17 specification](https://spec.m17project.org/). Changes to framing, addressing or M17_inet should follow the spec, and the issue or PR should cite the section involved. If the spec itself is unclear or wrong, raise it with the M17 Project as well.

## Questions

For questions and general discussion, find N1ADJ on the [M17 Project Discord](https://discord.gg/4brEP8wwVp).
