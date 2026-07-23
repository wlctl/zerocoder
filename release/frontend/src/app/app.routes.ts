import { Routes } from '@angular/router';

export const routes: Routes = [
  { path: '', redirectTo: 'uploads', pathMatch: 'full' },
  {
    path: 'uploads',
    loadComponent: () =>
      import('./pages/uploads-list/uploads-list.component').then((m) => m.UploadsListComponent),
  },
  {
    path: 'viewer/:id',
    loadComponent: () =>
      import('./pages/viewer/viewer.component').then((m) => m.ViewerComponent),
  },
  {
    path: 'window/:fileId',
    loadComponent: () =>
      import('./pages/file-window/file-window.component').then((m) => m.FileWindowComponent),
  },
  { path: '**', redirectTo: 'uploads' },
];