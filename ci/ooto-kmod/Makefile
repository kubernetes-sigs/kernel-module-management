KERNEL_SRC_DIR =

ccflags-y += -I$(KERNEL_SRC_DIR)/include
obj-m += ooto_ci.o

all:
	make -C $(KERNEL_SRC_DIR) $(MAKE_OPTS) M=$(PWD) modules

