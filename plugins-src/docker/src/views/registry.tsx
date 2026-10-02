import type { Route } from '../router';
import { AlertsPage } from './AlertsPage';
import { AutoUpdatePage } from './AutoUpdatePage';
import { CleanupPage } from './CleanupPage';
import { ContainerPage } from './ContainerPage';
import { ContainersPage } from './ContainersPage';
import { CreatePage } from './CreatePage';
import { ImagesPage } from './ImagesPage';
import { NetworksPage } from './NetworksPage';
import { RegistriesPage } from './RegistriesPage';
import { StackPage } from './StackPage';
import { StacksPage } from './StacksPage';
import { TemplatePage } from './TemplatePage';
import { TemplatesPage } from './TemplatesPage';
import { VolumesPage } from './VolumesPage';

/** Maps a route to its component. The shell renders this inside its content area. */
export function ViewHost({ route }: { route: Route }) {
  switch (route.view) {
    case 'containers':
      return <ContainersPage />;
    case 'container':
      return <ContainerPage id={route.id} tab={route.tab} />;
    case 'stacks':
      return <StacksPage />;
    case 'stack':
      return <StackPage name={route.name} />;
    case 'create':
      return <CreatePage from={route.from} image={route.image} />;
    case 'templates':
      return <TemplatesPage />;
    case 'template':
      return <TemplatePage id={route.id} />;
    case 'images':
      return <ImagesPage />;
    case 'volumes':
      return <VolumesPage />;
    case 'networks':
      return <NetworksPage />;
    case 'registries':
      return <RegistriesPage />;
    case 'cleanup':
      return <CleanupPage />;
    case 'autoupdate':
      return <AutoUpdatePage />;
    case 'alerts':
      return <AlertsPage />;
  }
}
