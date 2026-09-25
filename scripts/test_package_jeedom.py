"""Offline Jeedom archive regressions; inputs and outputs are temporary fixtures."""
from contextlib import redirect_stdout
from datetime import datetime, timezone
import hashlib
import importlib.util
import io
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile


SPEC = importlib.util.spec_from_file_location('package_jeedom', Path(__file__).with_name('package-jeedom.py'))
PACKAGER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PACKAGER)
STAMP = int(datetime(2026, 9, 25, 8, 10, 12, tzinfo=timezone.utc).timestamp())


class JeedomPackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='ajaxbridge-package-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bundle = self.root / 'Jeedom'
        self.output = self.root / 'release'
        self.contents = {
            'README.md': b'Fixture instructions\n',
            '.gitattributes': b'* text eol=lf\n',
            'files/core/example.php': b'<?php\necho "fixture";\n',
            'manifest.json': b'{"fixture":true}\n',
            'validation/.fixture': b'Hidden validation fixture\n',
        }
        for relative, content in self.contents.items():
            source = self.bundle / relative
            source.parent.mkdir(parents=True, exist_ok=True)
            source.write_bytes(content)
        self.write_checksums()
        for target in ('socket.socket.connect', 'socket.create_connection', 'socket.getaddrinfo'):
            blocker = patch(target, side_effect=AssertionError('Network is forbidden in packaging tests'))
            blocker.start()
            self.addCleanup(blocker.stop)

    def checksum_line(self, relative, content):
        return hashlib.sha256(content).hexdigest() + '  ' + relative + '\n'

    def write_checksums(self):
        data = ''.join(self.checksum_line(name, content) for name, content in sorted(self.contents.items()))
        (self.bundle / 'SHA256SUMS').write_text(data, encoding='utf-8', newline='\n')

    def package(self, **kwargs):
        with redirect_stdout(io.StringIO()):
            return PACKAGER.package(self.root, kwargs.get('version', '2.1.0'),
                                    kwargs.get('timestamp', STAMP), kwargs.get('output', self.output))

    def assert_rejected_without_archive(self, **kwargs):
        with self.assertRaises((ValueError, OSError)):
            self.package(**kwargs)
        self.assertFalse(self.output.exists(), 'Invalid input must be rejected before creating an archive')

    def test_exact_bytes_hidden_files_checksums_and_zip_integrity(self):
        archive = self.package()
        self.assertEqual(archive.name, 'AjaxBridge_2.1.0_jeedom_patch.zip')
        expected = {**self.contents, 'SHA256SUMS': (self.bundle / 'SHA256SUMS').read_bytes()}
        with zipfile.ZipFile(archive) as stream:
            self.assertIsNone(stream.testzip())
            self.assertEqual(stream.namelist(), ['Jeedom/' + name for name in sorted(expected)])
            for name, content in expected.items():
                with self.subTest(name=name):
                    info = stream.getinfo('Jeedom/' + name)
                    self.assertEqual(stream.read(info), content)
                    self.assertEqual(info.date_time, (2026, 9, 25, 8, 10, 12))
                    self.assertEqual(info.external_attr >> 16, 0o100644)
                    self.assertEqual(info.create_system, 3)
                    self.assertEqual(info.compress_type, zipfile.ZIP_DEFLATED)

    def test_reproducible_across_filesystem_times_modes_and_output_directories(self):
        first = self.package().read_bytes()
        for name in self.contents:
            source = self.bundle / name
            os.utime(source, (STAMP - 500000, STAMP - 500000))
            source.chmod(0o600)
        second = self.package(output=self.root / 'another-release').read_bytes()
        self.assertEqual(hashlib.sha256(first).digest(), hashlib.sha256(second).digest())
        self.assertEqual(first, second)

    def test_modified_payload_is_rejected(self):
        (self.bundle / 'files/core/example.php').write_bytes(b'<?php echo "tampered";')
        self.assert_rejected_without_archive()

    def test_missing_manifest_file_is_rejected(self):
        (self.bundle / 'manifest.json').unlink()
        self.assert_rejected_without_archive()

    def test_unlisted_visible_and_hidden_files_are_rejected(self):
        for name in ('unexpected.txt', '.private-cache'):
            with self.subTest(name=name):
                source = self.bundle / name
                source.write_bytes(b'Not in the distribution manifest')
                try:
                    self.assert_rejected_without_archive()
                finally:
                    source.unlink()

    def test_traversal_absolute_and_backslash_paths_are_rejected(self):
        outside = self.root / 'outside.txt'
        outside.write_bytes(b'Must never enter the archive')
        for path in ('../outside.txt', 'files/../../outside.txt', '/outside.txt', '..\\outside.txt'):
            with self.subTest(path=path):
                self.write_checksums()
                with (self.bundle / 'SHA256SUMS').open('a', encoding='utf-8') as stream:
                    stream.write(self.checksum_line(path, outside.read_bytes()))
                self.assert_rejected_without_archive()

    def test_duplicate_checksum_path_is_rejected(self):
        with (self.bundle / 'SHA256SUMS').open('a', encoding='utf-8') as stream:
            stream.write(self.checksum_line('README.md', self.contents['README.md']))
        self.assert_rejected_without_archive()

    def test_invalid_checksum_and_noncanonical_alias_are_rejected(self):
        original = (self.bundle / 'SHA256SUMS').read_text(encoding='utf-8')
        variants = [original.replace(original[:64], 'x' * 64, 1),
                    original.replace('  README.md', '  ./README.md'),
                    original.replace('  README.md', '  files/../README.md')]
        for content in variants:
            with self.subTest(content=content):
                (self.bundle / 'SHA256SUMS').write_text(content, encoding='utf-8')
                self.assert_rejected_without_archive()

    def test_symlink_to_outside_bundle_is_rejected(self):
        outside = self.root / 'outside.txt'
        outside.write_bytes(self.contents['README.md'])
        source = self.bundle / 'README.md'
        source.unlink()
        try:
            source.symlink_to(outside)
        except (OSError, NotImplementedError) as error:
            self.skipTest('Symlink creation is unavailable on this platform: ' + str(error))
        self.assert_rejected_without_archive()

    def test_checksum_manifest_cannot_be_a_symlink_outside_bundle(self):
        checksums = self.bundle / 'SHA256SUMS'
        outside = self.root / 'external-checksums'
        outside.write_bytes(checksums.read_bytes())
        checksums.unlink()
        try:
            checksums.symlink_to(outside)
        except (OSError, NotImplementedError) as error:
            self.skipTest('Symlink creation is unavailable on this platform: ' + str(error))
        self.assert_rejected_without_archive()

    def test_bundle_root_cannot_be_a_symlink(self):
        real_bundle = self.root / 'real-bundle'
        self.bundle.rename(real_bundle)
        try:
            self.bundle.symlink_to(real_bundle, target_is_directory=True)
        except (OSError, NotImplementedError) as error:
            self.skipTest('Symlink creation is unavailable on this platform: ' + str(error))
        self.assert_rejected_without_archive()

    def test_empty_distribution_is_rejected(self):
        self.root = self.root / 'empty-fixture'
        self.bundle = self.root / 'Jeedom'
        self.bundle.mkdir(parents=True)
        (self.bundle / 'SHA256SUMS').write_text('', encoding='utf-8')
        self.assert_rejected_without_archive()

    def test_version_cannot_control_output_path(self):
        for version in ('v2.1.0', '../2.1.0', '2.1.0/../../escape', '', '2.1'):
            with self.subTest(version=version):
                self.assert_rejected_without_archive(version=version)

    def test_zip_timestamp_range_is_enforced(self):
        for timestamp in (0, int(datetime(2108, 1, 1, tzinfo=timezone.utc).timestamp())):
            with self.subTest(timestamp=timestamp):
                self.assert_rejected_without_archive(timestamp=timestamp)


if __name__ == '__main__':
    unittest.main()
