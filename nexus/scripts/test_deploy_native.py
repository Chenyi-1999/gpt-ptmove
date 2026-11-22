"""Offline native-upgrade isolation/rollback tests; no SSH, sockets or credentials."""
import json
import os
from pathlib import Path
import unittest
from unittest import mock

import deploy_native as deploy


class NativeUpgradeTests(unittest.TestCase):
    root = Path('/opt/gpt-ptmove-nexus')

    def setUp(self):
        resolve_version = mock.patch.object(deploy, 'release_version', return_value='1.0.0')
        resolve_version.start()
        self.addCleanup(resolve_version.stop)

    def container(self):
        return {'Image': 'sha256:previous-native', 'Config': {'Labels': {
            'com.docker.compose.project': 'ptmove-nexus',
            'com.docker.compose.service': 'ptmove-nexus'}},
            'Mounts': [{'Destination': '/app/data', 'Source': str(self.root / 'data')}]}

    def output(self, container):
        return [json.dumps([container]).encode(), b'{"version":"0.3.0"}']

    def test_rejects_foreign_project_before_mutation(self):
        container = self.container()
        container['Config']['Labels']['com.docker.compose.project'] = 'unrelated'
        with mock.patch.object(deploy, 'health'), \
             mock.patch.object(deploy.subprocess, 'check_output', side_effect=self.output(container)), \
             mock.patch.object(deploy, 'prepare') as prepare, mock.patch.object(deploy, 'compose') as compose:
            with self.assertRaises(SystemExit):
                deploy.upgrade(self.root)
            prepare.assert_not_called()
            compose.assert_not_called()

    def test_rejects_foreign_data_mount(self):
        container = self.container()
        container['Mounts'][0]['Source'] = '/opt/another-project/data'
        with mock.patch.object(deploy, 'health'), \
             mock.patch.object(deploy.subprocess, 'check_output', side_effect=self.output(container)), \
             mock.patch.object(deploy, 'prepare') as prepare, mock.patch.object(deploy, 'compose') as compose:
            with self.assertRaises(SystemExit):
                deploy.upgrade(self.root)
            prepare.assert_not_called()
            compose.assert_not_called()

    def test_failed_new_health_rolls_back_only_native_image(self):
        with mock.patch.object(deploy, 'health', side_effect=[{}, RuntimeError('unhealthy'), {}]) as health, \
             mock.patch.object(deploy.subprocess, 'check_output', side_effect=self.output(self.container())), \
             mock.patch.object(deploy, 'prepare'), mock.patch.object(deploy, 'compose') as compose:
            with self.assertRaises(RuntimeError):
                deploy.upgrade(self.root)
            self.assertEqual(compose.call_args_list, [
                mock.call(self.root, 'production', 'up', '-d', '--no-build'),
                mock.call(self.root, 'production', 'up', '-d', '--no-build', image='sha256:previous-native')])
            health.assert_has_calls([mock.call(18121, '1.0.0'), mock.call(18120, '1.0.0'), mock.call(18120, '0.3.0')])

    def test_failed_canary_prevents_inspection_and_upgrade(self):
        with mock.patch.object(deploy, 'health', side_effect=RuntimeError('canary unhealthy')), \
             mock.patch.object(deploy.subprocess, 'check_output') as inspect, \
             mock.patch.object(deploy, 'compose') as compose:
            with self.assertRaises(RuntimeError):
                deploy.upgrade(self.root)
            inspect.assert_not_called()
            compose.assert_not_called()

    def test_compose_pins_data_env_ports_and_image_despite_ambient_overrides(self):
        injected = {"PTMOVE_DATA_DIR": "/opt/unrelated/data", "PTMOVE_ENV_FILE": "/opt/unrelated/.env",
                    "PTMOVE_BIND": "81.70.24.5", "PTMOVE_PORT": "12345", "PTMOVE_IMAGE": "foreign:latest"}
        for kind, data, port in (("production", "data", "18120"), ("canary", "canary-data", "18121")):
            with mock.patch.dict(os.environ, injected), \
                 mock.patch.object(deploy, 'command') as command:
                deploy.compose(self.root, kind, 'up', '-d', '--no-build')
                environment = command.call_args.kwargs['env']
                self.assertEqual(environment['PTMOVE_DATA_DIR'], str(self.root / data))
                self.assertEqual(environment['PTMOVE_ENV_FILE'], str(self.root / ('.env.canary' if kind == 'canary' else '.env.ptmove')))
                self.assertEqual(environment['PTMOVE_PORT'], port)
                self.assertEqual(environment['PTMOVE_BIND'], '0.0.0.0')
                self.assertEqual(environment['PTMOVE_IMAGE'], 'ptmove-nexus:1.0.0')
                deploy.compose(self.root, kind, 'up', '-d', '--no-build', image='sha256:previous-native')
                self.assertEqual(command.call_args.kwargs['env']['PTMOVE_IMAGE'], 'sha256:previous-native')


if __name__ == '__main__':
    unittest.main()
