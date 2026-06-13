import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import {
  createService as createServiceRequest,
  deleteService as deleteServiceRequest,
  getHealthz,
  getInventory,
  getService,
  listPlanes,
  listServices,
  queryKeys,
  updateService as updateServiceRequest,
} from "./api/controlPlane";
import type { ServiceResource } from "./api/types";
import { Shell } from "./components/layout/Shell";
import { OverviewPanel } from "./features/dashboard/OverviewPanel";
import { PlanesPanel } from "./features/planes/PlanesPanel";
import {
  defaultCreateServiceForm,
  defaultEditServiceForm,
  editFormFromService,
  type ServiceEditFormState,
  type ServiceFormState,
  toCreateServicePayload,
  toUpdateServicePayload,
} from "./features/services/forms";
import { ServiceDetailPanel } from "./features/services/ServiceDetailPanel";
import { ServicesPanel } from "./features/services/ServicesPanel";

const adminTokenStorageKey = "mini-cloud-admin-token";

function App() {
  const queryClient = useQueryClient();
  const [adminToken, setAdminToken] = useState(
    () => window.localStorage.getItem(adminTokenStorageKey) ?? "",
  );
  const [selectedServiceID, setSelectedServiceID] = useState("");
  const [serviceForm, setServiceForm] = useState<ServiceFormState>(
    defaultCreateServiceForm(),
  );
  const [editForm, setEditForm] = useState<ServiceEditFormState>(
    defaultEditServiceForm(),
  );
  const [editFormSourceServiceID, setEditFormSourceServiceID] = useState("");
  const [isEditFormDirty, setIsEditFormDirty] = useState(false);
  const hasAdminToken = adminToken.trim() !== "";

  const healthQuery = useQuery({
    queryKey: queryKeys.healthz,
    queryFn: getHealthz,
    refetchInterval: 5_000,
  });

  useEffect(() => {
    const token = adminToken.trim();
    if (token === "") {
      window.localStorage.removeItem(adminTokenStorageKey);
    } else {
      window.localStorage.setItem(adminTokenStorageKey, token);
    }
    void invalidateControlPlaneQueries(queryClient);
  }, [queryClient, adminToken]);

  const inventoryQuery = useQuery({
    queryKey: queryKeys.inventory,
    queryFn: () => getInventory(adminToken),
    enabled: hasAdminToken,
    refetchInterval: 10_000,
  });

  const planesQuery = useQuery({
    queryKey: queryKeys.planes,
    queryFn: () => listPlanes(adminToken),
    enabled: hasAdminToken,
    refetchInterval: 10_000,
  });

  const servicesQuery = useQuery({
    queryKey: queryKeys.services,
    queryFn: () => listServices(adminToken),
    enabled: hasAdminToken,
    refetchInterval: 10_000,
  });

  const planes = planesQuery.data?.items ?? [];
  const services = servicesQuery.data?.items ?? [];
  const selectedOrFirstServiceID =
    selectedServiceID || services[0]?.metadata.id || "";

  const serviceDetailQuery = useQuery({
    queryKey: queryKeys.service(selectedOrFirstServiceID),
    queryFn: () => getService(adminToken, selectedOrFirstServiceID),
    enabled: selectedOrFirstServiceID !== "" && hasAdminToken,
    refetchInterval: 10_000,
  });

  const currentService = serviceDetailQuery.data ?? null;

  useEffect(() => {
    if (planes.length === 0 || serviceForm.planeID !== "") {
      return;
    }
    setServiceForm((current) => ({ ...current, planeID: planes[0].id }));
  }, [planes, serviceForm.planeID]);

  useEffect(() => {
    if (services.length === 0) {
      if (selectedServiceID !== "") {
        setSelectedServiceID("");
      }
      return;
    }
    if (selectedServiceID === "") {
      setSelectedServiceID(services[0].metadata.id);
      return;
    }
    if (!services.some((item) => item.metadata.id === selectedServiceID)) {
      queryClient.removeQueries({
        queryKey: queryKeys.service(selectedServiceID),
      });
      setSelectedServiceID(services[0].metadata.id);
    }
  }, [queryClient, services, selectedServiceID]);

  useEffect(() => {
    if (!currentService) {
      return;
    }
    const serviceChanged =
      currentService.metadata.id !== editFormSourceServiceID;
    if (!serviceChanged && isEditFormDirty) {
      return;
    }
    setEditForm(editFormFromService(currentService));
    setEditFormSourceServiceID(currentService.metadata.id);
    setIsEditFormDirty(false);
  }, [currentService, editFormSourceServiceID, isEditFormDirty]);

  const createService = useMutation({
    mutationFn: (form: ServiceFormState) =>
      createServiceRequest(adminToken, toCreateServicePayload(form)),
    onSuccess: async (response) => {
      setServiceForm((current) => ({
        ...defaultCreateServiceForm(),
        planeID: current.planeID,
      }));
      setSelectedServiceID(response.metadata.id);
      await invalidateServiceArea(queryClient, response.metadata.id);
    },
  });

  const updateService = useMutation({
    mutationFn: (input: { serviceID: string; form: ServiceEditFormState }) =>
      updateServiceRequest(
        adminToken,
        input.serviceID,
        toUpdateServicePayload(input.form),
      ),
    onSuccess: async (response) => {
      queryClient.setQueryData<ServiceResource>(
        queryKeys.service(response.metadata.id),
        response,
      );
      setEditForm(editFormFromService(response));
      setEditFormSourceServiceID(response.metadata.id);
      setIsEditFormDirty(false);
      await invalidateServiceArea(queryClient, response.metadata.id);
    },
  });

  const deleteService = useMutation({
    mutationFn: (serviceID: string) =>
      deleteServiceRequest(adminToken, serviceID),
    onSuccess: async (response) => {
      queryClient.setQueryData<ServiceResource>(
        queryKeys.service(response.metadata.id),
        response,
      );
      await invalidateServiceArea(queryClient, response.metadata.id);
    },
  });

  const updateEditFormField = (
    field: keyof ServiceEditFormState,
    value: string,
  ) => {
    setEditForm((current) => ({
      ...current,
      [field]: value,
    }));
    setIsEditFormDirty(true);
  };

  return (
    <Shell adminToken={adminToken} onAdminTokenChange={setAdminToken}>
      <OverviewPanel
        hasAdminToken={hasAdminToken}
        healthQuery={healthQuery}
        inventory={inventoryQuery.data}
        inventoryError={inventoryQuery.error}
      />
      <PlanesPanel
        planes={planes}
        inventory={inventoryQuery.data}
        error={planesQuery.error}
      />
      <ServicesPanel
        hasAdminToken={hasAdminToken}
        services={services}
        planes={planes}
        selectedServiceID={selectedOrFirstServiceID}
        form={serviceForm}
        isCreating={createService.isPending}
        createError={createService.error}
        listError={servicesQuery.error}
        onFormChange={setServiceForm}
        onCreate={() => createService.mutate(serviceForm)}
        onSelectService={setSelectedServiceID}
      />
      <ServiceDetailPanel
        service={currentService}
        planes={planes}
        form={editForm}
        hasAdminToken={hasAdminToken}
        isUpdating={updateService.isPending}
        isDeleting={deleteService.isPending}
        updateError={updateService.error}
        deleteError={deleteService.error}
        detailError={serviceDetailQuery.error}
        onFormChange={updateEditFormField}
        onUpdate={() => {
          if (!currentService) {
            return;
          }
          updateService.mutate({
            serviceID: currentService.metadata.id,
            form: editForm,
          });
        }}
        onDelete={() => {
          if (!currentService) {
            return;
          }
          deleteService.mutate(currentService.metadata.id);
        }}
      />
    </Shell>
  );
}

async function invalidateControlPlaneQueries(
  queryClient: ReturnType<typeof useQueryClient>,
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.inventory }),
    queryClient.invalidateQueries({ queryKey: queryKeys.planes }),
    queryClient.invalidateQueries({ queryKey: queryKeys.services }),
  ]);
}

async function invalidateServiceArea(
  queryClient: ReturnType<typeof useQueryClient>,
  serviceID?: string,
) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.inventory }),
    queryClient.invalidateQueries({ queryKey: queryKeys.planes }),
    queryClient.invalidateQueries({ queryKey: queryKeys.services }),
    serviceID
      ? queryClient.invalidateQueries({
          queryKey: queryKeys.service(serviceID),
        })
      : Promise.resolve(),
  ]);
}

export default App;
