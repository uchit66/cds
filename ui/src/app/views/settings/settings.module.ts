import {NgModule, CUSTOM_ELEMENTS_SCHEMA} from '@angular/core';
import {SharedModule} from '../../shared/shared.module';
import {RouterModule} from '@angular/router';
import {SettingsRouting} from './settings.routing';
import {SettingsComponent} from './settings.component';
import {ActionAddComponent} from './action/add/action.add.component';
import {ActionEditComponent} from './action/edit/action.edit.component';
import {ActionListComponent} from './action/list/action.list.component';
import {DownloadComponent} from './download/download.component';
import {GroupEditComponent} from './group/edit/group.edit.component';
import {GroupListComponent} from './group/list/group.list.component';
import {UserEditComponent} from './user/edit/user.edit.component';
import {UserListComponent} from './user/list/user.list.component';
import {WorkerModelAddComponent} from './worker-model/add/worker-model.add.component';
import {WorkerModelEditComponent} from './worker-model/edit/worker-model.edit.component';
import {WorkerModelListComponent} from './worker-model/list/worker-model.list.component';
import {StatusComponent} from './status/status.component';
import {QueueComponent} from './queue/queue.component';

@NgModule({
    declarations: [
        SettingsComponent,
        ActionAddComponent,
        ActionEditComponent,
        ActionListComponent,
        DownloadComponent,
        GroupEditComponent,
        GroupListComponent,
        UserEditComponent,
        UserListComponent,
        WorkerModelAddComponent,
        WorkerModelEditComponent,
        WorkerModelListComponent,
        StatusComponent,
        QueueComponent
    ],
    imports: [
      SharedModule,
      RouterModule,
      SettingsRouting
    ],
    schemas: [
        CUSTOM_ELEMENTS_SCHEMA
    ]
})
export class SettingsModule {
}
